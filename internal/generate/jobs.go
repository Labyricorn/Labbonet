package generate

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

// Update is what a job subscriber receives. A subscription always starts with one
// "snapshot" (the text generated so far), then live "event" updates, then one "end".
type Update struct {
	Kind         string // "snapshot", "event" or "end"
	GenerationID int64
	Event        providers.Event
	Content      string
	Reasoning    string
	// Set on "end": the stored outcome of the reply.
	Status       string
	ErrorKind    string
	FinishReason string
}

const (
	subscriberBuffer = 1024
	finishedRetain   = 2 * time.Minute
	finishedKeep     = 16
)

// Job is one running or recently finished generation. It owns no database state; the
// Runner persists everything. Its job is to outlive the HTTP request that started it:
// any number of requests may attach, detach and re-attach while it runs.
type Job struct {
	GenerationID   int64
	ConversationID int64

	mu        sync.Mutex
	content   strings.Builder
	reasoning strings.Builder
	subs      map[*Subscription]struct{}
	done      bool
	end       Update
	doneAt    time.Time
}

// Subscription delivers a job's updates. C is closed after the "end" update, or early if
// the reader fell too far behind to be kept (it can subscribe again for a fresh snapshot).
type Subscription struct {
	C   chan Update
	job *Job
}

func newJob(generationID, conversationID int64) *Job {
	return &Job{GenerationID: generationID, ConversationID: conversationID, subs: map[*Subscription]struct{}{}}
}

// Subscribe attaches a reader. The snapshot and the live stream are taken under one lock
// so no text is lost or repeated between them.
func (j *Job) Subscribe() *Subscription {
	j.mu.Lock()
	defer j.mu.Unlock()
	sub := &Subscription{C: make(chan Update, subscriberBuffer), job: j}
	sub.C <- Update{Kind: "snapshot", GenerationID: j.GenerationID, Content: j.content.String(), Reasoning: j.reasoning.String()}
	if j.done {
		sub.C <- j.end
		close(sub.C)
		return sub
	}
	j.subs[sub] = struct{}{}
	return sub
}

// Close detaches the reader. It never affects the generation.
func (s *Subscription) Close() {
	s.job.mu.Lock()
	defer s.job.mu.Unlock()
	if _, ok := s.job.subs[s]; ok {
		delete(s.job.subs, s)
		close(s.C)
	}
}

func (j *Job) broadcastLocked(u Update) {
	for sub := range j.subs {
		select {
		case sub.C <- u:
		default:
			delete(j.subs, sub)
			close(sub.C)
		}
	}
}

func (j *Job) publish(ev providers.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if ev.Kind == providers.EventDelta {
		j.content.WriteString(ev.Content)
		j.reasoning.WriteString(ev.Reasoning)
	}
	j.broadcastLocked(Update{Kind: "event", GenerationID: j.GenerationID, Event: ev})
}

func (j *Job) finish(end Update) {
	j.mu.Lock()
	defer j.mu.Unlock()
	end.Kind, end.GenerationID = "end", j.GenerationID
	j.done, j.end, j.doneAt = true, end, time.Now()
	j.broadcastLocked(end)
	for sub := range j.subs {
		delete(j.subs, sub)
		close(sub.C)
	}
}

// Running reports whether the generation is still in progress.
func (j *Job) Running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return !j.done
}

// Jobs runs generations detached from the requests that start them, so closing a tab or
// navigating away does not stop a reply. Explicit Stop goes through Runner.Registry.
type Jobs struct {
	Runner *Runner

	mu     sync.Mutex
	byGen  map[int64]*Job
	byConv map[int64]*Job
	recent []*Job
	wg     sync.WaitGroup
}

// NewJobs builds a job service around rn.
func NewJobs(rn *Runner) *Jobs {
	return &Jobs{Runner: rn, byGen: map[int64]*Job{}, byConv: map[int64]*Job{}}
}

type started struct {
	job *Job
	err error
}

// Start begins a generation and returns once its turn is stored (or it was refused). The
// request's own context is deliberately not used: the job lives until the provider
// finishes, it is stopped, or the process shuts down.
func (js *Jobs) Start(req Request) (*Job, error) {
	res := make(chan started, 1)
	js.wg.Add(1)
	go func() {
		defer js.wg.Done()
		var job *Job
		err := js.Runner.Run(context.Background(), req, func(id int64) {
			job = newJob(id, req.ConversationID)
			js.mu.Lock()
			js.byGen[id], js.byConv[req.ConversationID] = job, job
			js.mu.Unlock()
			res <- started{job: job}
		}, func(ev providers.Event) error {
			if job != nil {
				job.publish(ev)
			}
			return nil
		})
		if job == nil {
			res <- started{err: err}
			return
		}
		job.finish(js.outcome(job, err))
		js.retire(job)
	}()
	r := <-res
	return r.job, r.err
}

// outcome reads the stored result so every subscriber sees what was actually recorded.
func (js *Jobs) outcome(job *Job, runErr error) Update {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	end := Update{Status: "error"}
	if g, err := js.Runner.Store.GetGeneration(ctx, job.GenerationID); err == nil {
		end.ErrorKind, end.FinishReason = g.ErrorKind, g.FinishReason
		if m, err := js.Runner.Store.GetMessage(ctx, g.MessageID); err == nil {
			end.Status = m.Status
		}
	} else if runErr == nil {
		end.Status = "complete"
	}
	return end
}

func (js *Jobs) retire(job *Job) {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.byConv[job.ConversationID] == job {
		delete(js.byConv, job.ConversationID)
	}
	js.recent = append(js.recent, job)
	js.pruneLocked()
}

func (js *Jobs) pruneLocked() {
	cut := 0
	for i, j := range js.recent {
		if len(js.recent)-cut > finishedKeep || time.Since(j.doneAt) > finishedRetain {
			delete(js.byGen, j.GenerationID)
			cut = i + 1
		}
	}
	js.recent = js.recent[cut:]
}

// Get returns a running or recently finished job. After the retention window the stored
// message is the only record, and callers fall back to reading it.
func (js *Jobs) Get(generationID int64) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	js.pruneLocked()
	return js.byGen[generationID]
}

// Active returns the conversation's running job, if any.
func (js *Jobs) Active(conversationID int64) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.byConv[conversationID]
}

// Wait blocks until every running job has stored its final state or ctx ends. Call it
// after Registry.CancelAll during shutdown so partial text is written before the
// database closes.
func (js *Jobs) Wait(ctx context.Context) {
	done := make(chan struct{})
	go func() { js.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
