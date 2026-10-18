package generate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/schedule"
	"github.com/Labyricorn/Labbonet/internal/store"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func TestSchedulerUnexpectedStartErrorIsRedacted(t *testing.T) {
	status, reason := classifyStart(errors.New("storage path and SECRET_SENTINEL"))
	if status != store.RunError || reason != "the scheduled reply could not start; check the thread and its settings" {
		t.Fatalf("unexpected error exposed: %s %s", status, reason)
	}
}

func TestStopBeforeGenerationRegistrationCancelsRegisteredReply(t *testing.T) {
	registry := NewRegistry()
	prepare, cancelPrepare := context.WithCancel(context.Background())
	defer cancelPrepare()
	sc := &Scheduler{Jobs: NewJobs(&Runner{Registry: registry}), active: map[int64]*activeRun{1: {prepare: prepare, cancelPrepare: cancelPrepare}}}
	if !sc.CancelRun(1) {
		t.Fatal("Stop did not mark the pending run")
	}
	if prepare.Err() == nil {
		t.Fatal("Stop did not cancel pre-turn preparation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.Register(7, cancel)
	sc.setGeneration(1, 7)
	if ctx.Err() == nil || !registry.WasCancelled(7) {
		t.Fatal("pending Stop did not cancel the newly registered generation")
	}
}

func TestShutdownBeforeGenerationRegistrationCancelsRegisteredReply(t *testing.T) {
	registry := NewRegistry()
	shutdown, stop := context.WithCancel(context.Background())
	stop()
	sc := &Scheduler{ctx: shutdown, Jobs: NewJobs(&Runner{Registry: registry}), active: map[int64]*activeRun{1: {}}}
	reply, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry.Register(7, cancel)
	sc.setGeneration(1, 7)
	if reply.Err() == nil {
		t.Fatal("shutdown missed the newly registered generation")
	}
}

func (c *fakeClock) now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) set(t time.Time) { c.mu.Lock(); c.t = t; c.mu.Unlock() }

func clockAt(t *testing.T, s string) *fakeClock {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeClock{t: v}
}

type schedFixture struct {
	rn    *Runner
	st    *store.Store
	jobs  *Jobs
	sch   *Scheduler
	clock *fakeClock
	ep    *store.Endpoint
	conv  *store.Conversation
}

func newSchedFixture(t *testing.T, chat string, delay time.Duration) *schedFixture {
	t.Helper()
	srv := scripted(t, chat, delay)
	t.Cleanup(srv.Close)
	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	jobs := NewJobs(rn)
	clock := clockAt(t, "2026-10-18T10:00:00Z")
	sch := &Scheduler{Store: st, Jobs: jobs, Now: clock.now}
	sch.active = map[int64]*activeRun{}
	t.Cleanup(func() {
		rn.Registry.CancelAll()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		jobs.Wait(ctx)
		sch.Wait(ctx)
	})
	return &schedFixture{rn: rn, st: st, jobs: jobs, sch: sch, clock: clock, ep: ep, conv: conv}
}

func (f *schedFixture) add(t *testing.T, mutate func(*schedule.Schedule)) schedule.Schedule {
	t.Helper()
	id := f.conv.ID
	s := schedule.Schedule{Name: "Morning", ConversationID: &id, Prompt: "Good morning", Kind: schedule.KindDaily, Timezone: "UTC", Time: "10:05", Enabled: true}
	if mutate != nil {
		mutate(&s)
	}
	saved, err := f.st.SaveSchedule(context.Background(), s, f.clock.now())
	if err != nil {
		t.Fatalf("save schedule: %v", err)
	}
	return saved
}

func (f *schedFixture) waitRun(t *testing.T, id int64) store.ScheduleRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, err := f.st.GetScheduleRun(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if r.FinishedAt != nil {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return store.ScheduleRun{}
}

func (f *schedFixture) runs(t *testing.T, scheduleID int64) []store.ScheduleRun {
	t.Helper()
	r, err := f.st.ListScheduleRuns(context.Background(), scheduleID, 50)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *schedFixture) messages(t *testing.T) []*store.Message {
	t.Helper()
	m, err := f.st.ListMessages(context.Background(), f.conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTickRunsADueScheduleOnceAndAdvances(t *testing.T) {
	f := newSchedFixture(t, sse("Hello", " there."), 0)
	s := f.add(t, nil)
	ctx := context.Background()

	if n := f.sch.Tick(ctx); n != 0 {
		t.Fatalf("ran %d before the due time", n)
	}
	f.clock.set(f.clock.now().Add(5*time.Minute + 20*time.Second)) // 10:05:20
	if n := f.sch.Tick(ctx); n != 1 {
		t.Fatalf("claimed %d, want 1", n)
	}
	if n := f.sch.Tick(ctx); n != 0 {
		t.Fatalf("second tick claimed %d for the same due time", n)
	}
	runs := f.runs(t, s.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	run := f.waitRun(t, runs[0].ID)
	if run.Status != store.RunComplete || run.Turns != 1 || run.Trigger != "schedule" {
		t.Fatalf("run = %+v", run)
	}
	msgs := f.messages(t)
	if len(msgs) != 2 || msgs[0].Content != "Good morning" || msgs[1].Content != "Hello there." {
		t.Fatalf("messages = %+v", msgs)
	}
	after, _ := f.st.GetSchedule(ctx, s.ID)
	want := time.Date(2026, 10, 19, 10, 5, 0, 0, time.UTC)
	if !after.Enabled || after.NextRun == nil || !after.NextRun.Equal(want) || after.RunCount != 1 || after.LastStatus != store.RunComplete {
		t.Fatalf("schedule after = %+v", after)
	}
}

func TestConcurrentTicksClaimADueTimeOnce(t *testing.T) {
	f := newSchedFixture(t, sse("ok."), 100*time.Millisecond)
	s := f.add(t, nil)
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := f.sch.Tick(context.Background())
			mu.Lock()
			total += n
			mu.Unlock()
		}()
	}
	wg.Wait()
	if total != 1 {
		t.Fatalf("claimed %d times", total)
	}
	runs := f.runs(t, s.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	f.waitRun(t, runs[0].ID)
	users := 0
	for _, m := range f.messages(t) {
		if m.Role == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user turns = %d", users)
	}
}

func TestMissedRunIsRecordedOnceAndNeverReplayed(t *testing.T) {
	f := newSchedFixture(t, sse("never."), 0)
	s := f.add(t, nil)
	// The server was off for three days.
	f.clock.set(f.clock.now().Add(72 * time.Hour))
	ctx := context.Background()
	if n := f.sch.Tick(ctx); n != 1 {
		t.Fatalf("claimed %d", n)
	}
	if n := f.sch.Tick(ctx); n != 0 {
		t.Fatalf("second tick claimed %d", n)
	}
	runs := f.runs(t, s.ID)
	if len(runs) != 1 || runs[0].Status != store.RunMissed || runs[0].FinishedAt == nil {
		t.Fatalf("runs = %+v", runs)
	}
	if len(f.messages(t)) != 0 {
		t.Fatal("a missed run sent a message")
	}
	after, _ := f.st.GetSchedule(ctx, s.ID)
	if after.NextRun == nil || !after.NextRun.After(f.clock.now()) || after.RunCount != 0 {
		t.Fatalf("schedule after = %+v", after)
	}
}

func TestOneOffScheduleSwitchesItselfOffAfterFiring(t *testing.T) {
	f := newSchedFixture(t, sse("done."), 0)
	s := f.add(t, func(s *schedule.Schedule) { s.Kind, s.Date = schedule.KindOnce, "2026-10-18" })
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	ctx := context.Background()
	if f.sch.Tick(ctx) != 1 {
		t.Fatal("did not fire")
	}
	f.waitRun(t, f.runs(t, s.ID)[0].ID)
	after, _ := f.st.GetSchedule(ctx, s.ID)
	if after.Enabled || after.NextRun != nil {
		t.Fatalf("one-off after firing = %+v", after)
	}
	f.clock.set(f.clock.now().Add(24 * time.Hour))
	if f.sch.Tick(ctx) != 0 || len(f.runs(t, s.ID)) != 1 {
		t.Fatal("one-off fired twice")
	}
}

func TestNotLoadedModelIsSkippedAndNeverCalled(t *testing.T) {
	f := newSchedFixture(t, sse("should not run."), 0)
	setModel(t, f.st, f.ep.ID, "m", "llm", "not-loaded", 4000)
	s := f.add(t, nil)
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	f.sch.Tick(context.Background())
	run := f.waitRun(t, f.runs(t, s.ID)[0].ID)
	if run.Status != store.RunSkipped || run.Reason == "" {
		t.Fatalf("run = %+v", run)
	}
	if len(f.messages(t)) != 0 {
		t.Fatal("a turn was stored for a model that is not loaded")
	}
}

func TestBusyThreadIsSkipped(t *testing.T) {
	f := newSchedFixture(t, sse("slow."), 700*time.Millisecond)
	s := f.add(t, nil)
	ctx := context.Background()
	if _, err := f.jobs.Start(Request{ConversationID: f.conv.ID, UserContent: "hello"}); err != nil {
		t.Fatal(err)
	}
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	f.sch.Tick(ctx)
	runs := f.runs(t, s.ID)
	if len(runs) != 1 || runs[0].Status != store.RunSkipped {
		t.Fatalf("runs = %+v", runs)
	}
}

func TestDeletedThreadIsSkippedAndReported(t *testing.T) {
	f := newSchedFixture(t, sse("x."), 0)
	s := f.add(t, nil)
	ctx := context.Background()
	if err := f.st.DeleteConversation(ctx, f.conv.ID); err != nil {
		t.Fatal(err)
	}
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	if f.sch.Tick(ctx) != 1 {
		t.Fatal("not handled")
	}
	runs := f.runs(t, s.ID)
	if len(runs) != 1 || runs[0].Status != store.RunSkipped || runs[0].Reason != "the thread was deleted" {
		t.Fatalf("runs = %+v", runs)
	}
	got, _ := f.st.GetSchedule(ctx, s.ID)
	if got.ConversationID != nil {
		t.Fatal("schedule still points at a deleted thread")
	}
	if _, err := f.st.SaveSchedule(ctx, got, f.clock.now()); err == nil {
		t.Fatal("saved a schedule without a thread")
	}
}

func TestAutoContinueRunsABoundedChain(t *testing.T) {
	f := newSchedFixture(t, sse("Part."), 0)
	s := f.add(t, func(s *schedule.Schedule) { s.AutoContinue = 2 })
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	f.sch.Tick(context.Background())
	run := f.waitRun(t, f.runs(t, s.ID)[0].ID)
	if run.Status != store.RunComplete || run.Turns != 3 {
		t.Fatalf("run = %+v", run)
	}
	msgs := f.messages(t)
	if len(msgs) != 6 || msgs[2].Content != schedule.ContinuePrompt || msgs[4].Content != schedule.ContinuePrompt {
		t.Fatalf("messages = %d: %+v", len(msgs), msgs)
	}
}

func TestCancelRunStopsReplyAndChain(t *testing.T) {
	f := newSchedFixture(t, sse("slow."), 5*time.Second)
	s := f.add(t, func(s *schedule.Schedule) { s.AutoContinue = 3 })
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	f.sch.Tick(context.Background())
	runs := f.runs(t, s.ID)
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.sch.mu.Lock()
		active := f.sch.active[runs[0].ID]
		registered := active != nil && active.generation != 0
		f.sch.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reply never registered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !f.sch.CancelRun(runs[0].ID) {
		t.Fatal("one Stop failed")
	}
	// This case cancels an existing reply; the pre-registration cases above
	// separately cover cancellation before a turn has been written.
	run := f.waitRun(t, runs[0].ID)
	if run.Status != store.RunCancelled {
		t.Fatalf("run = %+v", run)
	}
	users := 0
	for _, m := range f.messages(t) {
		if m.Role == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user turns after cancel = %d", users)
	}
}

func TestRunNowKeepsTheScheduleAndRefusesWhileRunning(t *testing.T) {
	f := newSchedFixture(t, sse("manual."), 400*time.Millisecond)
	s := f.add(t, func(s *schedule.Schedule) { s.Enabled = false })
	ctx := context.Background()
	run, err := f.sch.RunNow(ctx, s.ID)
	if err != nil || run.Trigger != "manual" {
		t.Fatalf("run now = %+v %v", run, err)
	}
	if _, err := f.sch.RunNow(ctx, s.ID); err == nil {
		t.Fatal("second run-now while running was accepted")
	}
	done := f.waitRun(t, run.ID)
	if done.Status != store.RunComplete {
		t.Fatalf("run = %+v", done)
	}
	after, _ := f.st.GetSchedule(ctx, s.ID)
	if after.Enabled || after.NextRun != nil || after.Revision != s.Revision {
		t.Fatalf("run-now changed the schedule: %+v", after)
	}
}

func TestSearchRequestWithoutSearchIsAnErrorEntry(t *testing.T) {
	f := newSchedFixture(t, sse("x."), 0)
	s := f.add(t, func(s *schedule.Schedule) { s.WebSearch = true })
	f.clock.set(f.clock.now().Add(6 * time.Minute))
	f.sch.Tick(context.Background())
	run := f.waitRun(t, f.runs(t, s.ID)[0].ID)
	if run.Status != store.RunError || len(f.messages(t)) != 0 {
		t.Fatalf("run = %+v, messages = %d", run, len(f.messages(t)))
	}
}

func TestStartRecoversRunsLeftRunning(t *testing.T) {
	f := newSchedFixture(t, sse("x."), 0)
	s := f.add(t, nil)
	ctx := context.Background()
	due := *s.NextRun
	if _, ok, err := f.st.ClaimRun(ctx, s, due, f.clock.now().Add(6*time.Minute), "schedule", store.RunRunning, ""); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	n, err := f.sch.Start()
	if err != nil || n != 1 {
		t.Fatalf("recovered %d, %v", n, err)
	}
	f.sch.Stop()
	runs := f.runs(t, s.ID)
	if len(runs) != 1 || runs[0].Status != store.RunInterrupted {
		t.Fatalf("runs = %+v", runs)
	}
}
