// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior reference (a timer that sends a stored prompt to a thread, with a status
// ledger): GobboNet js/22-scheduler.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f,
// Copyright (C) 2026 Elodine / GoblinCorps. No upstream source copied; unlike upstream
// the timer lives in the server, so it runs with the browser closed. docs/REUSE-LOG.md.
package generate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Labyricorn/Labbonet/internal/schedule"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Errors returned by RunNow.
var (
	ErrScheduleBusy  = errors.New("a reply is already being generated in that thread, or this schedule is already running")
	ErrScheduleNoRun = errors.New("this schedule has no thread to run in")
)

const (
	defaultTick  = 15 * time.Second
	defaultGrace = 5 * time.Minute
	// UnattendedReasoningLimit bounds a scheduled reply's reasoning so nobody has to be
	// present to stop a runaway model.
	UnattendedReasoningLimit = 60000
)

type activeRun struct {
	cancelled     bool
	generation    int64
	prepare       context.Context
	cancelPrepare context.CancelFunc
}

// Scheduler fires scheduled prompts. It lives in the server process, so it works with the
// browser closed but not with Labbonet closed: a due time that passes while the server is not
// running is recorded as missed once and never replayed. Runs reuse the Jobs service, so
// the same endpoint slots, model-state refusal and Stop apply; a scheduled run never loads a
// model and a chain of automatic follow-ups is bounded by the schedule's AutoContinue.
type Scheduler struct {
	Store *store.Store
	Jobs  *Jobs
	// Now, Interval and Grace default to the wall clock, 15 s and 5 min; tests override them.
	Now      func() time.Time
	Interval time.Duration
	Grace    time.Duration

	mu       sync.Mutex
	active   map[int64]*activeRun
	lastTick time.Time
	started  time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	loop     sync.WaitGroup
	runs     sync.WaitGroup
}

func (sc *Scheduler) now() time.Time {
	if sc.Now != nil {
		return sc.Now()
	}
	return time.Now()
}

func (sc *Scheduler) interval() time.Duration {
	if sc.Interval > 0 {
		return sc.Interval
	}
	return defaultTick
}

func (sc *Scheduler) grace() time.Duration {
	if sc.Grace > 0 {
		return sc.Grace
	}
	return defaultGrace
}

func ledgerCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// Start marks runs left over from a previous process as interrupted and begins ticking.
func (sc *Scheduler) Start() (recovered int, err error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.cancel != nil {
		return 0, nil
	}
	ctx, cancel := ledgerCtx()
	recovered, err = sc.Store.RecoverScheduleRuns(ctx, sc.now())
	cancel()
	if err != nil {
		return 0, err
	}
	sc.active = map[int64]*activeRun{}
	sc.ctx, sc.cancel = context.WithCancel(context.Background())
	sc.started = sc.now()
	sc.loop.Add(1)
	go func() {
		defer sc.loop.Done()
		sc.Tick(sc.ctx)
		t := time.NewTicker(sc.interval())
		defer t.Stop()
		for {
			select {
			case <-sc.ctx.Done():
				return
			case <-t.C:
				sc.Tick(sc.ctx)
			}
		}
	}()
	return recovered, nil
}

// Stop ends ticking so nothing new starts. Call it before cancelling generations.
func (sc *Scheduler) Stop() {
	sc.mu.Lock()
	cancel := sc.cancel
	sc.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	sc.loop.Wait()
}

// Wait blocks until every run goroutine has written its final state or ctx ends.
func (sc *Scheduler) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() { sc.runs.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Status is what the dashboard shows about the scheduler itself.
type SchedulerStatus struct {
	Running  bool       `json:"running"`
	Now      time.Time  `json:"now"`
	LastTick *time.Time `json:"last_tick"`
	Active   int        `json:"active_runs"`
	TickSecs int        `json:"tick_seconds"`
	GraceSec int        `json:"grace_seconds"`
}

// Status reports whether the timer is ticking.
func (sc *Scheduler) Status() SchedulerStatus {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	st := SchedulerStatus{Running: sc.cancel != nil && sc.ctx.Err() == nil, Now: sc.now().UTC(), Active: len(sc.active),
		TickSecs: int(sc.interval() / time.Second), GraceSec: int(sc.grace() / time.Second)}
	if !sc.lastTick.IsZero() {
		t := sc.lastTick.UTC()
		st.LastTick = &t
	}
	return st
}

// Tick handles every schedule that is due now and returns how many it claimed. Exported so
// tests can drive the clock.
func (sc *Scheduler) Tick(ctx context.Context) int {
	now := sc.now()
	sc.mu.Lock()
	sc.lastTick = now
	sc.mu.Unlock()
	due, err := sc.Store.DueSchedules(ctx, now)
	if err != nil {
		return 0
	}
	claimed := 0
	for _, s := range due {
		if ctx.Err() != nil {
			break
		}
		if sc.handleDue(ctx, s, now) {
			claimed++
		}
	}
	return claimed
}

func (sc *Scheduler) handleDue(ctx context.Context, s schedule.Schedule, now time.Time) bool {
	due := *s.NextRun
	switch {
	case now.Sub(due) > sc.grace():
		// Missed: record one entry and move on to the next future time. Occurrences skipped
		// in between are not replayed in a burst.
		_, ok, _ := sc.Store.ClaimRun(ctx, s, due, now, "schedule", store.RunMissed,
			"Labbonet was not running (or was busy) at the scheduled time; this run was skipped, not replayed")
		return ok
	case s.ConversationID == nil:
		_, ok, _ := sc.Store.ClaimRun(ctx, s, due, now, "schedule", store.RunSkipped, "the thread was deleted")
		return ok
	}
	if job := sc.Jobs.Active(*s.ConversationID); job != nil && job.Running() {
		_, ok, _ := sc.Store.ClaimRun(ctx, s, due, now, "schedule", store.RunSkipped, "a reply was already being generated in that thread")
		return ok
	}
	id, ok, err := sc.Store.ClaimRun(ctx, s, due, now, "schedule", store.RunRunning, "")
	if err != nil || !ok {
		return false
	}
	sc.launch(id, s)
	return true
}

// RunNow starts a schedule immediately without changing its next time.
func (sc *Scheduler) RunNow(ctx context.Context, scheduleID int64) (store.ScheduleRun, error) {
	s, err := sc.Store.GetSchedule(ctx, scheduleID)
	if err != nil {
		return store.ScheduleRun{}, err
	}
	if s.ConversationID == nil {
		return store.ScheduleRun{}, ErrScheduleNoRun
	}
	if job := sc.Jobs.Active(*s.ConversationID); job != nil && job.Running() {
		return store.ScheduleRun{}, ErrScheduleBusy
	}
	now := sc.now()
	id, ok, err := sc.Store.ClaimRun(ctx, s, now, now, "manual", store.RunRunning, "")
	if errors.Is(err, store.ErrConflict) || (err == nil && !ok) {
		return store.ScheduleRun{}, ErrScheduleBusy
	}
	if err != nil {
		return store.ScheduleRun{}, err
	}
	sc.launch(id, s)
	return sc.Store.GetScheduleRun(ctx, id)
}

// CancelRun stops a running entry: its current reply is cancelled and no follow-up starts.
func (sc *Scheduler) CancelRun(runID int64) bool {
	sc.mu.Lock()
	ar := sc.active[runID]
	if ar == nil {
		sc.mu.Unlock()
		return false
	}
	ar.cancelled = true
	gen := ar.generation
	cancelPrepare := ar.cancelPrepare
	sc.mu.Unlock()
	if cancelPrepare != nil {
		cancelPrepare()
	}
	if gen != 0 {
		sc.Jobs.Runner.Registry.Cancel(gen)
	}
	return true
}

func (sc *Scheduler) launch(runID int64, s schedule.Schedule) {
	sc.mu.Lock()
	if sc.active == nil {
		sc.active = map[int64]*activeRun{}
	}
	parent := sc.ctx
	if parent == nil {
		parent = context.Background()
	}
	prepare, cancelPrepare := context.WithCancel(parent)
	sc.active[runID] = &activeRun{prepare: prepare, cancelPrepare: cancelPrepare}
	sc.mu.Unlock()
	sc.runs.Add(1)
	go func() {
		defer sc.runs.Done()
		defer cancelPrepare()
		status, reason := sc.execute(runID, s)
		sc.mu.Lock()
		delete(sc.active, runID)
		sc.mu.Unlock()
		ctx, cancel := ledgerCtx()
		defer cancel()
		_ = sc.Store.FinishRun(ctx, runID, status, reason, sc.now())
	}()
}

func (sc *Scheduler) cancelled(runID int64) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	ar := sc.active[runID]
	return ar != nil && ar.cancelled
}

func (sc *Scheduler) setGeneration(runID, gen int64) {
	sc.mu.Lock()
	cancelled := false
	if ar := sc.active[runID]; ar != nil {
		ar.generation = gen
		cancelled = ar.cancelled
	}
	if sc.ctx != nil && sc.ctx.Err() != nil {
		cancelled = true
	}
	sc.mu.Unlock()
	// Stop may arrive while Start is assembling the prompt, before the generation
	// has an id. Carry that cancellation forward once the reply is registered.
	if cancelled {
		sc.Jobs.Runner.Registry.Cancel(gen)
	}
}

// execute sends the prompt and any bounded follow-ups, returning the ledger outcome.
func (sc *Scheduler) execute(runID int64, s schedule.Schedule) (string, string) {
	sc.mu.Lock()
	prepare := sc.active[runID].prepare
	sc.mu.Unlock()
	conv := *s.ConversationID
	prompt := s.Prompt
	turns := 0
	for {
		if sc.cancelled(runID) {
			return store.RunCancelled, "stopped before the next turn"
		}
		if sc.ctx != nil && sc.ctx.Err() != nil {
			return store.RunInterrupted, "Labbonet was shutting down"
		}
		req := Request{ConversationID: conv, UserContent: prompt, ReasoningLimitChars: UnattendedReasoningLimit}
		// Search applies to the scheduled prompt only, never to the follow-up turns.
		req.WebSearch = turns == 0 && s.WebSearch
		job, err := sc.Jobs.StartContext(prepare, req)
		if err != nil {
			if sc.cancelled(runID) {
				return store.RunCancelled, "stopped before the next turn"
			}
			if sc.ctx != nil && sc.ctx.Err() != nil {
				return store.RunInterrupted, "Labbonet was shutting down"
			}
			status, reason := classifyStart(err)
			if turns > 0 && status == store.RunSkipped {
				status = store.RunError
			}
			return status, reason
		}
		sc.setGeneration(runID, job.GenerationID)
		gen := job.GenerationID
		ctx, cancel := ledgerCtx()
		_ = sc.Store.UpdateRunProgress(ctx, runID, turns, &gen)
		cancel()
		end := sc.Jobs.await(job)
		turns++
		ctx, cancel = ledgerCtx()
		_ = sc.Store.UpdateRunProgress(ctx, runID, turns, &gen)
		cancel()
		switch {
		case sc.cancelled(runID) || end.Status == "cancelled":
			return store.RunCancelled, "stopped by you"
		case sc.ctx != nil && sc.ctx.Err() != nil:
			return store.RunInterrupted, "Labbonet was shutting down"
		case end.Status != "complete":
			reason := "the reply ended with status " + end.Status
			if end.ErrorKind != "" {
				reason += " (" + end.ErrorKind + ")"
			}
			return store.RunError, reason
		}
		if turns > s.AutoContinue {
			return store.RunComplete, ""
		}
		// A chain stops if the schedule was switched off, changed or deleted meanwhile.
		ctx, cancel = ledgerCtx()
		cur, gerr := sc.Store.GetSchedule(ctx, s.ID)
		cancel()
		if gerr != nil || cur.Revision != s.Revision {
			return store.RunCancelled, "the schedule was edited or deleted during the chain"
		}
		prompt = schedule.ContinuePrompt
	}
}

// await returns the stored outcome once the job has ended.
func (js *Jobs) await(job *Job) Update {
	for {
		sub := job.Subscribe()
		for u := range sub.C {
			if u.Kind == "end" {
				sub.Close()
				return u
			}
		}
		if !job.Running() {
			return js.outcome(job, nil)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// classifyStart turns a refusal before the turn exists into a ledger status. Anything that
// means "not now, not safely" is skipped; everything else is an error.
func classifyStart(err error) (string, string) {
	switch {
	case errors.Is(err, ErrModelNotLoaded):
		return store.RunSkipped, "the thread's model is not loaded; scheduled runs never load a model"
	case errors.Is(err, ErrEndpointDisabled):
		return store.RunSkipped, "the thread's endpoint is disabled"
	case errors.Is(err, ErrNoModelSelected):
		return store.RunSkipped, "the thread has no endpoint or model selected"
	case errors.Is(err, sql.ErrNoRows):
		return store.RunSkipped, "the thread or its endpoint was removed"
	case errors.Is(err, store.ErrConflict):
		return store.RunSkipped, "the thread changed or was generating"
	case errors.Is(err, ErrSearch):
		return store.RunError, fmt.Sprintf("web search was refused: %v", err)
	}
	return store.RunError, "the scheduled reply could not start; check the thread and its settings"
}
