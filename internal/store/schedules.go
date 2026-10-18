package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/schedule"
)

// ErrInvalidSchedule reports a rejected schedule.
var ErrInvalidSchedule = errors.New("invalid schedule request")

const runsPerSchedule = 50

// Run statuses recorded in the ledger.
const (
	RunRunning     = "running"
	RunComplete    = "complete"
	RunError       = "error"
	RunCancelled   = "cancelled"
	RunMissed      = "missed"
	RunSkipped     = "skipped"
	RunInterrupted = "interrupted"
)

func invalidSchedule(err error) error { return fmt.Errorf("%w: %v", ErrInvalidSchedule, err) }

const scheduleColumns = `s.id,s.name,s.conversation_id,s.prompt,s.kind,s.timezone,s.run_date,s.run_time,s.enabled,s.web_search,s.auto_continue,s.next_run,s.revision,
	(SELECT COUNT(*) FROM schedule_runs r WHERE r.schedule_id=s.id AND r.status IN ('complete','error','cancelled','interrupted')),
	COALESCE((SELECT r.status FROM schedule_runs r WHERE r.schedule_id=s.id ORDER BY r.id DESC LIMIT 1),''),
	COALESCE((SELECT r.started_at FROM schedule_runs r WHERE r.schedule_id=s.id ORDER BY r.id DESC LIMIT 1),'')`

func scanSchedule(sc interface{ Scan(...any) error }) (schedule.Schedule, error) {
	var s schedule.Schedule
	var next sql.NullInt64
	var enabled, web int
	var lastAt string
	if err := sc.Scan(&s.ID, &s.Name, &s.ConversationID, &s.Prompt, &s.Kind, &s.Timezone, &s.Date, &s.Time, &enabled, &web, &s.AutoContinue, &next, &s.Revision, &s.RunCount, &s.LastStatus, &lastAt); err != nil {
		return s, err
	}
	s.Enabled, s.WebSearch = enabled == 1, web == 1
	if next.Valid {
		t := time.Unix(next.Int64, 0).UTC()
		s.NextRun = &t
	}
	if lastAt != "" {
		if t, err := parseTime(lastAt); err == nil {
			s.LastRunAt = &t
		}
	}
	return s, nil
}

// ListSchedules returns every schedule, oldest first.
func (s *Store) ListSchedules(ctx context.Context) ([]schedule.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules s ORDER BY s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schedule.Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// GetSchedule returns one schedule (sql.ErrNoRows if absent).
func (s *Store) GetSchedule(ctx context.Context, id int64) (schedule.Schedule, error) {
	return scanSchedule(s.db.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM schedules s WHERE s.id=?`, id))
}

// DueSchedules returns enabled schedules whose next run is at or before now.
func (s *Store) DueSchedules(ctx context.Context, now time.Time) ([]schedule.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM schedules s WHERE s.enabled=1 AND s.next_run IS NOT NULL AND s.next_run<=? ORDER BY s.next_run,s.id`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []schedule.Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SaveSchedule validates and saves a schedule. A new schedule has ID 0; an edit must carry the
// revision it was loaded at. An enabled schedule must have a future run; saving recomputes it
// from now, so editing the time of a missed schedule never replays the old time.
func (s *Store) SaveSchedule(ctx context.Context, in schedule.Schedule, now time.Time) (schedule.Schedule, error) {
	if err := in.Validate(); err != nil {
		return in, invalidSchedule(err)
	}
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM conversations WHERE id=?`, *in.ConversationID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return in, invalidSchedule(errors.New("that thread no longer exists"))
		}
		return in, err
	}
	var next any
	if in.Enabled {
		t, ok := in.Next(now)
		if !ok {
			return in, invalidSchedule(errors.New("that time has already passed; pick a future time or save the schedule switched off"))
		}
		next = t.Unix()
	}
	stamp := formatTime(now)
	if in.ID == 0 {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules`).Scan(&n); err != nil {
			return in, err
		}
		if n >= schedule.MaxSchedules {
			return in, invalidSchedule(fmt.Errorf("at most %d schedules", schedule.MaxSchedules))
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO schedules(name,conversation_id,prompt,kind,timezone,run_date,run_time,enabled,web_search,auto_continue,next_run,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.Name, *in.ConversationID, in.Prompt, in.Kind, in.Timezone, in.Date, in.Time, b2i(in.Enabled), b2i(in.WebSearch), in.AutoContinue, next, stamp, stamp)
		if err != nil {
			return in, err
		}
		id, _ := res.LastInsertId()
		return s.GetSchedule(ctx, id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE schedules SET name=?,conversation_id=?,prompt=?,kind=?,timezone=?,run_date=?,run_time=?,enabled=?,web_search=?,auto_continue=?,next_run=?,revision=revision+1,updated_at=?
		WHERE id=? AND revision=?`, in.Name, *in.ConversationID, in.Prompt, in.Kind, in.Timezone, in.Date, in.Time, b2i(in.Enabled), b2i(in.WebSearch), in.AutoContinue, next, stamp, in.ID, in.Revision)
	if err != nil {
		return in, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM schedules WHERE id=?`, in.ID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return in, sql.ErrNoRows
		}
		return in, ErrConflict
	}
	return s.GetSchedule(ctx, in.ID)
}

// DuplicateSchedule copies a schedule, switched off, under a "copy" name.
func (s *Store) DuplicateSchedule(ctx context.Context, id int64, now time.Time) (schedule.Schedule, error) {
	src, err := s.GetSchedule(ctx, id)
	if err != nil {
		return src, err
	}
	base := []rune(src.Name)
	if len(base) > schedule.MaxNameRunes-5 {
		base = base[:schedule.MaxNameRunes-5]
	}
	src.ID, src.Revision, src.Name, src.Enabled = 0, 0, strings.TrimSpace(string(base))+" copy", false
	return s.SaveSchedule(ctx, src, now)
}

// DeleteSchedule removes a schedule and its ledger if revision still matches (0 skips the check).
func (s *Store) DeleteSchedule(ctx context.Context, id, revision int64) error {
	q, args := `DELETE FROM schedules WHERE id=?`, []any{id}
	if revision > 0 {
		q, args = q+` AND revision=?`, append(args, revision)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var one int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM schedules WHERE id=?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return ErrConflict
	}
	return nil
}

// ---- run ledger -----------------------------------------------------------------------

// ScheduleRun is one ledger entry.
type ScheduleRun struct {
	ID             int64      `json:"id"`
	ScheduleID     int64      `json:"schedule_id"`
	ConversationID *int64     `json:"conversation_id"`
	Trigger        string     `json:"trigger"`
	DueAt          time.Time  `json:"due_at"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	Turns          int        `json:"turns"`
	GenerationID   *int64     `json:"generation_id"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

const runColumns = `id,schedule_id,conversation_id,trigger,due_at,status,reason,turns,generation_id,started_at,finished_at`

func scanRun(sc interface{ Scan(...any) error }) (ScheduleRun, error) {
	var r ScheduleRun
	var due int64
	var started string
	var finished sql.NullString
	if err := sc.Scan(&r.ID, &r.ScheduleID, &r.ConversationID, &r.Trigger, &due, &r.Status, &r.Reason, &r.Turns, &r.GenerationID, &started, &finished); err != nil {
		return r, err
	}
	r.DueAt = time.Unix(due, 0).UTC()
	r.StartedAt, _ = parseTime(started)
	if finished.Valid {
		t, _ := parseTime(finished.String)
		r.FinishedAt = &t
	}
	return r, nil
}

// ClaimRun records that a due time is being handled and advances the schedule, in one
// transaction, so a time can be claimed once however many ticks, tabs or restarts see it.
// trigger is "schedule" (a due time) or "manual" (run now; the schedule is not advanced and
// a schedule that is already running refuses with ErrConflict). status "running" starts a
// run; any other status (missed, skipped) records a finished entry. claimed is false when
// another claim won.
func (s *Store) ClaimRun(ctx context.Context, sch schedule.Schedule, due, now time.Time, trigger, status, reason string) (id int64, claimed bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	switch trigger {
	case "schedule":
		var enabled, nextVal any = 1, nil
		if sch.Kind == schedule.KindDaily {
			if t, ok := sch.Next(now); ok {
				nextVal = t.Unix()
			}
		}
		if nextVal == nil {
			enabled = 0
		}
		res, err := tx.ExecContext(ctx, `UPDATE schedules SET next_run=?,enabled=? WHERE id=? AND enabled=1 AND next_run=? AND revision=?`, nextVal, enabled, sch.ID, due.Unix(), sch.Revision)
		if err != nil {
			return 0, false, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return 0, false, nil
		}
	case "manual":
		var revision int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM schedules WHERE id=?`, sch.ID).Scan(&revision); err != nil {
			return 0, false, err
		}
		if revision != sch.Revision {
			return 0, false, ErrConflict
		}
		var busy int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM schedule_runs WHERE schedule_id=? AND status='running' LIMIT 1`, sch.ID).Scan(&busy)
		if err == nil {
			return 0, false, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, false, err
		}
	default:
		return 0, false, fmt.Errorf("unknown run trigger %q", trigger)
	}
	var finished any
	if status != RunRunning {
		finished = formatTime(now)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO schedule_runs(schedule_id,conversation_id,trigger,due_at,status,reason,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?)`,
		sch.ID, sch.ConversationID, trigger, due.Unix(), status, reason, formatTime(now), finished)
	if err != nil {
		return 0, false, err
	}
	id, _ = res.LastInsertId()
	if _, err = tx.ExecContext(ctx, `DELETE FROM schedule_runs WHERE schedule_id=? AND id NOT IN (SELECT id FROM schedule_runs WHERE schedule_id=? ORDER BY id DESC LIMIT ?)`, sch.ID, sch.ID, runsPerSchedule); err != nil {
		return 0, false, err
	}
	return id, true, tx.Commit()
}

// UpdateRunProgress records the turns finished so far and the latest generation.
func (s *Store) UpdateRunProgress(ctx context.Context, id int64, turns int, generationID *int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE schedule_runs SET turns=?,generation_id=? WHERE id=? AND status='running'`, turns, generationID, id)
	return err
}

// FinishRun sets the terminal status of a running entry. Reasons are bounded.
func (s *Store) FinishRun(ctx context.Context, id int64, status, reason string, now time.Time) error {
	if r := []rune(reason); len(r) > 300 {
		reason = string(r[:299]) + "…"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE schedule_runs SET status=?,reason=?,finished_at=? WHERE id=? AND status='running'`, status, reason, formatTime(now), id)
	return err
}

// ListScheduleRuns returns the newest entries first.
func (s *Store) ListScheduleRuns(ctx context.Context, scheduleID int64, limit int) ([]ScheduleRun, error) {
	if limit <= 0 || limit > runsPerSchedule {
		limit = runsPerSchedule
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM schedule_runs WHERE schedule_id=? ORDER BY id DESC LIMIT ?`, scheduleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetScheduleRun returns one entry (sql.ErrNoRows if absent).
func (s *Store) GetScheduleRun(ctx context.Context, id int64) (ScheduleRun, error) {
	return scanRun(s.db.QueryRowContext(ctx, `SELECT `+runColumns+` FROM schedule_runs WHERE id=?`, id))
}

// RecoverScheduleRuns marks runs left "running" by a previous process as interrupted. A run is
// never resumed or repeated.
func (s *Store) RecoverScheduleRuns(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE schedule_runs SET status='interrupted',reason='Labbonet stopped while this run was in progress',finished_at=? WHERE status='running'`, formatTime(now))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
