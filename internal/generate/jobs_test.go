package generate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
	"github.com/Labyricorn/Labbonet/internal/store"
)

func quote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func sse(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(`data: {"choices":[{"delta":{"content":` + quote(c) + `},"finish_reason":null}]}` + "\n\n")
	}
	b.WriteString(`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"model":"m"}` + "\n\n" + "data: [DONE]\n\n")
	return b.String()
}

func reasoningSSE(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(`data: {"choices":[{"delta":{"reasoning_content":` + quote(c) + `},"finish_reason":null}]}` + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func noop(providers.Event) error { return nil }

func scripted(t *testing.T, body string, delay time.Duration) *fakeprovider.Server {
	t.Helper()
	return fakeprovider.New(fakeprovider.Script{ChatSSE: body, ChatFirstByteDelay: delay, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
}

func drain(t *testing.T, sub *Subscription) (snapshot Update, text string, end Update) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	first := true
	for {
		select {
		case u, ok := <-sub.C:
			if !ok {
				t.Fatal("subscription closed before an end update")
			}
			switch u.Kind {
			case "snapshot":
				if !first {
					t.Fatal("snapshot must be first")
				}
				snapshot = u
			case "event":
				text += u.Event.Content
			case "end":
				return snapshot, text, u
			}
			first = false
		case <-timeout:
			t.Fatal("timed out waiting for the job to end")
		}
	}
}

func TestReplyCapEndsReplyAtSentence(t *testing.T) {
	lead := strings.Repeat("a", 100)
	srv := scripted(t, sse(lead, ". This is", " more. And more."), 0)
	defer srv.Close()
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	job, err := NewJobs(rn).Start(Request{ConversationID: conv.ID, UserContent: "hi", MaxReplyChars: 100})
	if err != nil {
		t.Fatal(err)
	}
	snap, streamed, end := drain(t, job.Subscribe())
	msgs, _ := st.ListMessages(context.Background(), conv.ID)
	if end.Status != "complete" || end.FinishReason != "reply_cap" || msgs[1].Content != lead+"." || snap.Content+streamed != lead+"." {
		t.Fatalf("end=%+v stored=%q snapshot=%q streamed=%q", end, msgs[1].Content, snap.Content, streamed)
	}
}

func TestReasoningWatchdogStopsRunawayReasoning(t *testing.T) {
	srv := scripted(t, reasoningSSE(strings.Repeat("think ", 100), strings.Repeat("more ", 100), strings.Repeat("again ", 200)), 0)
	defer srv.Close()
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi", ReasoningLimitChars: 1000}, nil, noop)
	if err == nil {
		t.Fatal("expected the watchdog error")
	}
	msgs, _ := st.ListMessages(context.Background(), conv.ID)
	if msgs[1].Status != "error" || len(msgs[1].Reasoning) == 0 || len(msgs[1].Reasoning) > 1000 {
		t.Fatalf("assistant = status %q reasoning %d chars", msgs[1].Status, len(msgs[1].Reasoning))
	}
	gens, _ := st.ListGenerationsForMessage(context.Background(), msgs[1].ID)
	if gens[0].ErrorKind != "reasoning_limit" || gens[0].FinishReason != "reasoning_limit" {
		t.Fatalf("generation = %+v", gens[0])
	}
}

func TestInvalidLimitsAndAttachmentsAreRejectedBeforeAnyTurn(t *testing.T) {
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, "http://127.0.0.1:1", "m")
	for _, req := range []Request{
		{ConversationID: conv.ID, UserContent: "hi", MaxReplyChars: 3},
		{ConversationID: conv.ID, UserContent: "hi", Attachments: []Attachment{{Name: "a", Content: "\x00"}}},
	} {
		err := rn.Run(context.Background(), req, nil, noop)
		if !errors.Is(err, ErrLimits) && !errors.Is(err, ErrAttachment) {
			t.Fatalf("err = %v", err)
		}
	}
	if msgs, _ := st.ListMessages(context.Background(), conv.ID); len(msgs) != 0 {
		t.Fatalf("rejected requests stored %d messages", len(msgs))
	}
}

func TestAttachmentIsStoredInTheUserMessageNotProvenance(t *testing.T) {
	srv := scripted(t, sse("ok"), 0)
	defer srv.Close()
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "see file", Attachments: []Attachment{{Name: "notes.md", Content: "SECRET-BODY"}}}, nil, noop)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := st.ListMessages(context.Background(), conv.ID)
	if !strings.Contains(msgs[0].Content, "[Attached file: notes.md]") || !strings.Contains(msgs[0].Content, "SECRET-BODY") {
		t.Fatalf("user message = %q", msgs[0].Content)
	}
	gens, _ := st.ListGenerationsForMessage(context.Background(), msgs[1].ID)
	// The composed text appears in the prompt snapshot and UserContent once each at most;
	// the attachment list itself is reduced to names.
	if !strings.Contains(gens[0].ParamsJSON, `"Attached":["notes.md"]`) || strings.Contains(gens[0].ParamsJSON, `"Attachments":[{`) {
		t.Fatalf("params = %s", gens[0].ParamsJSON)
	}
}

// The reply keeps running with no reader attached, and a later subscriber gets a
// snapshot of what exists plus the rest, with no second turn created.
func TestJobSurvivesDetachAndReattaches(t *testing.T) {
	srv := scripted(t, sse("Hello"), 300*time.Millisecond)
	defer srv.Close()
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	jobs := NewJobs(rn)
	job, err := jobs.Start(Request{ConversationID: conv.ID, UserContent: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	job.Subscribe().Close() // the tab goes away
	if jobs.Active(conv.ID) != job || jobs.Get(job.GenerationID) != job {
		t.Fatal("job not tracked while running")
	}
	// A second turn while the first is streaming is refused, never duplicated.
	if _, err := jobs.Start(Request{ConversationID: conv.ID, UserContent: "again"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second send err = %v, want conflict", err)
	}
	_, _, end := drain(t, job.Subscribe())
	if end.Status != "complete" {
		t.Fatalf("end = %+v", end)
	}
	msgs, _ := st.ListMessages(context.Background(), conv.ID)
	if len(msgs) != 2 || msgs[1].Content != "Hello" {
		t.Fatalf("messages = %+v", msgs)
	}
	// Re-attaching after the end still delivers the snapshot and outcome.
	snap, _, end2 := drain(t, job.Subscribe())
	if snap.Content != "Hello" || end2.Status != "complete" {
		t.Fatalf("late subscriber snapshot=%+v end=%+v", snap, end2)
	}
	if jobs.Active(conv.ID) != nil {
		t.Fatal("finished job still active")
	}
}

func mustAssistant(t *testing.T, st *store.Store, convID int64) int64 {
	t.Helper()
	msgs, err := st.ListMessages(context.Background(), convID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages %v %v", msgs, err)
	}
	return msgs[1].ID
}

func TestStopWorksOnAQueuedJob(t *testing.T) {
	srv := scripted(t, sse("slow"), 800*time.Millisecond)
	defer srv.Close()
	rn, st := newRunner(t)
	ep, convA := mustEndpointAndConversation(t, st, srv.URL, "m")
	model := "m"
	convB, err := st.CreateConversation(context.Background(), "b", &ep.ID, &model, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewJobs(rn)
	a, err := jobs.Start(Request{ConversationID: convA.ID, UserContent: "first"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := jobs.Start(Request{ConversationID: convB.ID, UserContent: "second"}) // concurrency limit 1: waits for the slot
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if !rn.Registry.Cancel(b.GenerationID) {
		t.Fatal("queued job not registered for cancel")
	}
	_, _, endB := drain(t, b.Subscribe())
	_, _, endA := drain(t, a.Subscribe())
	if endB.Status != "cancelled" || endA.Status != "complete" {
		t.Fatalf("queued end = %+v, first end = %+v", endB, endA)
	}
	gens, _ := st.ListGenerationsForMessage(context.Background(), mustAssistant(t, st, convB.ID))
	if gens[0].FirstTokenAt != nil {
		t.Fatal("cancelled queued job reached the provider")
	}
}

func TestSameEndpointJobsRunOneAtATime(t *testing.T) {
	srv := scripted(t, sse("x"), 300*time.Millisecond)
	defer srv.Close()
	rn, st := newRunner(t)
	ep, convA := mustEndpointAndConversation(t, st, srv.URL, "m")
	model := "m"
	convB, _ := st.CreateConversation(context.Background(), "b", &ep.ID, &model, "", "{}")
	jobs := NewJobs(rn)
	a, _ := jobs.Start(Request{ConversationID: convA.ID, UserContent: "1"})
	b, _ := jobs.Start(Request{ConversationID: convB.ID, UserContent: "2"})
	drain(t, a.Subscribe())
	drain(t, b.Subscribe())
	ga, _ := st.ListGenerationsForMessage(context.Background(), mustAssistant(t, st, convA.ID))
	gb, _ := st.ListGenerationsForMessage(context.Background(), mustAssistant(t, st, convB.ID))
	if ga[0].FinishedAt == nil || gb[0].FirstTokenAt == nil || gb[0].FirstTokenAt.Add(50*time.Millisecond).Before(*ga[0].FinishedAt) {
		t.Fatalf("second generation started before the first finished: %+v / %+v", ga[0], gb[0])
	}
}

func TestShutdownCancelKeepsPartialAndWaitReturns(t *testing.T) {
	srv := scripted(t, sse("x"), 2*time.Second)
	defer srv.Close()
	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	jobs := NewJobs(rn)
	if _, err := jobs.Start(Request{ConversationID: conv.ID, UserContent: "hi"}); err != nil {
		t.Fatal(err)
	}
	rn.Registry.CancelAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jobs.Wait(ctx)
	if ctx.Err() != nil {
		t.Fatal("jobs did not finish after CancelAll")
	}
	msgs, _ := st.ListMessages(context.Background(), conv.ID)
	if msgs[1].Status != "cancelled" {
		t.Fatalf("status = %q", msgs[1].Status)
	}
}
