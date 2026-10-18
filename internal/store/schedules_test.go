package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/profile"
	"github.com/Labyricorn/Labbonet/internal/schedule"
	_ "modernc.org/sqlite"
)

func schedFor(t *testing.T, st *Store) (schedule.Schedule, int64) {
	t.Helper()
	conv, err := st.CreateConversation(t.Context(), "c", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	id := conv.ID
	return schedule.Schedule{Name: "S", ConversationID: &id, Prompt: "hi", Kind: schedule.KindDaily, Timezone: "UTC", Time: "10:05", Enabled: true}, id
}

func TestClaimRunIsCompareAndSwapOnTheDueTime(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	base, _ := schedFor(t, st)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	s, err := st.SaveSchedule(ctx, base, now)
	if err != nil {
		t.Fatal(err)
	}
	due := *s.NextRun
	late := due.Add(10 * time.Second)

	id, ok, err := st.ClaimRun(ctx, s, due, late, "schedule", RunRunning, "")
	if err != nil || !ok || id == 0 {
		t.Fatalf("first claim = %d %v %v", id, ok, err)
	}
	// The same stale view of the schedule cannot claim the same due time again.
	if _, ok, err = st.ClaimRun(ctx, s, due, late, "schedule", RunRunning, ""); err != nil || ok {
		t.Fatalf("second claim = %v %v", ok, err)
	}
	after, _ := st.GetSchedule(ctx, s.ID)
	if after.Revision != s.Revision || !after.NextRun.Equal(due.Add(24*time.Hour)) {
		t.Fatalf("after = %+v", after)
	}
	// A manual run is refused while one is running, then allowed once it has finished.
	if _, _, err = st.ClaimRun(ctx, after, late, late, "manual", RunRunning, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("manual while running = %v", err)
	}
	if err = st.FinishRun(ctx, id, RunComplete, "", late); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = st.ClaimRun(ctx, after, late, late, "manual", RunRunning, ""); err != nil || !ok {
		t.Fatalf("manual after finish = %v %v", ok, err)
	}
	// Saving a schedule recomputes the next run from now: a time that passed is never replayed.
	edited := after
	edited.Time = "09:00"
	saved, err := st.SaveSchedule(ctx, edited, late)
	if err != nil || !saved.NextRun.After(late) {
		t.Fatalf("edit = %+v %v", saved, err)
	}
}

func TestEditedScheduleCannotBeClaimedFromStaleSnapshot(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := t.Context()
	base, _ := schedFor(t, st)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	original, err := st.SaveSchedule(ctx, base, now)
	if err != nil {
		t.Fatal(err)
	}
	edited := original
	edited.Prompt = "a different prompt at the same time"
	if _, err = st.SaveSchedule(ctx, edited, now); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := st.ClaimRun(ctx, original, *original.NextRun, *original.NextRun, "schedule", RunRunning, ""); err != nil || claimed {
		t.Fatalf("stale timed claim = %v, %v", claimed, err)
	}
	if _, _, err := st.ClaimRun(ctx, original, now, now, "manual", RunRunning, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale manual claim = %v", err)
	}
	runs, err := st.ListScheduleRuns(ctx, original.ID, 50)
	if err != nil || len(runs) != 0 {
		t.Fatalf("stale claim wrote ledger: %+v, %v", runs, err)
	}
}

func TestRunLedgerIsBoundedAndCascades(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	base, _ := schedFor(t, st)
	base.Enabled = false
	s, err := st.SaveSchedule(ctx, base, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < runsPerSchedule+10; i++ {
		due := time.Unix(1_800_000_000+int64(i)*60, 0)
		if _, ok, err := st.ClaimRun(ctx, s, due, due, "manual", RunMissed, "x"); err != nil || !ok {
			t.Fatalf("claim %d = %v %v", i, ok, err)
		}
	}
	runs, _ := st.ListScheduleRuns(ctx, s.ID, 1000)
	if len(runs) != runsPerSchedule {
		t.Fatalf("ledger holds %d", len(runs))
	}
	if err = st.DeleteSchedule(ctx, s.ID, s.Revision); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = st.db.QueryRow(`SELECT COUNT(*) FROM schedule_runs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("runs after delete = %d %v", n, err)
	}
}

func TestDeletingAThreadKeepsTheScheduleWithoutAThread(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	base, cid := schedFor(t, st)
	s, err := st.SaveSchedule(ctx, base, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteConversation(ctx, cid); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSchedule(ctx, s.ID)
	if err != nil || got.ConversationID != nil {
		t.Fatalf("schedule = %+v %v", got, err)
	}
}

func TestFullResetClearsSchedulesAndProfilesOnly(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	base, _ := schedFor(t, st)
	if _, err := st.SaveSchedule(ctx, base, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveProfile(ctx, profile.Profile{Name: "P", Data: profile.Data{Prefs: profile.Prefs{Follow: true}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetData(ctx, "cast"); err != nil {
		t.Fatal(err)
	}
	if l, _ := st.ListSchedules(ctx); len(l) != 1 {
		t.Fatal("a cast reset removed schedules")
	}
	if err := st.ResetData(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	if l, _ := st.ListSchedules(ctx); len(l) != 0 {
		t.Error("schedules survived a full reset")
	}
	if l, _ := st.ListProfiles(ctx); len(l) != 0 {
		t.Error("profiles survived a full reset")
	}
}

func TestProfileStoreValidationAndRevisions(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	now := time.Now()
	p, err := st.SaveProfile(ctx, profile.Profile{Name: " Night ", Data: profile.Data{Appearance: "default"}}, now)
	if err != nil || p.Name != "Night" || p.Revision != 1 {
		t.Fatalf("create = %+v %v", p, err)
	}
	if _, err = st.SaveProfile(ctx, profile.Profile{Name: "night"}, now); !errors.Is(err, ErrInvalidProfile) {
		t.Errorf("duplicate name = %v", err)
	}
	for name, d := range map[string]profile.Data{
		"theme id": {Appearance: "has space"},
		"reply":    {Prefs: profile.Prefs{ReplyCap: 7}},
		"reason":   {Prefs: profile.Prefs{ReasoningLimit: 99}},
	} {
		if _, err = st.SaveProfile(ctx, profile.Profile{Name: name, Data: d}, now); !errors.Is(err, ErrInvalidProfile) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	p.Name = "Day"
	if _, err = st.SaveProfile(ctx, p, now); err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveProfile(ctx, p, now); !errors.Is(err, ErrConflict) {
		t.Errorf("stale save = %v", err)
	}
	if err = st.SetActiveProfile(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteProfile(ctx, p.ID, 0); err != nil {
		t.Fatal(err)
	}
	if id, _ := st.ActiveProfile(ctx); id != 0 {
		t.Errorf("active after delete = %d", id)
	}
	if err = st.DeleteProfile(ctx, p.ID, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("delete again = %v", err)
	}
}

// A populated schema-5 database upgrades to schema 6 with its data intact and a backup.
func TestScheduleMigrationPreservesExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range []string{"0001_init.sql", "0002_cast.sql", "0003_branches.sql", "0004_memory.sql", "0005_mods_search.sql"} {
		if err = applyMigration(db, migrationFile{i + 1, f}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO conversations(title,params_json,created_at,updated_at) VALUES('keep','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if convs, err := st.ListConversations(t.Context()); err != nil || len(convs) != 1 || convs[0].Title != "keep" {
		t.Fatalf("convs=%+v err=%v", convs, err)
	}
	if backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "*")); len(backups) != 1 {
		t.Fatalf("pre-upgrade backup missing: %v", backups)
	}
	if l, err := st.ListSchedules(t.Context()); err != nil || len(l) != 0 {
		t.Fatalf("schedules=%+v err=%v", l, err)
	}
	if id, err := st.ActiveProfile(t.Context()); err != nil || id != 0 {
		t.Fatalf("active profile=%d err=%v", id, err)
	}
}
