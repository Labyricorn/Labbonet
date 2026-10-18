package generate

import (
	"context"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

const runnerTestSSE = `data: {"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
	`data: {"choices":[{"delta":{},"finish_reason":"stop"}],"model":"reported-model","usage":{"prompt_tokens":10,"completion_tokens":2},"timings":{"predicted_per_second":9.5,"prompt_ms":100}}` + "\n\n" +
	"data: [DONE]\n\n"

func newRunner(t *testing.T) (*Runner, *store.Store) {
	t.Helper()
	st := storetest.New(t)
	return &Runner{
		Store:     st,
		Protector: secrets.NewProtector(),
		Egress:    egress.NewClient(),
		Slots:     NewSlots(),
		Registry:  NewRegistry(),
	}, st
}

func mustEndpointAndConversation(t *testing.T, st *store.Store, baseURL, modelID string) (*store.Endpoint, *store.Conversation) {
	t.Helper()
	ep, err := st.CreateEndpoint(context.Background(), "test", "openai", baseURL, "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	conv, err := st.CreateConversation(context.Background(), "chat", &ep.ID, &modelID, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	return ep, conv
}

// A-C11: provenance (endpoint, requested and reported model, params,
// start, first token, finish, usage, finish reason) ends up recorded,
// and the "Through the API alone... a conversation can stream" done
// criterion holds end to end through Runner.
func TestRunner_CompletesAndRecordsProvenance(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srv.Close()

	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "requested-model")

	var events []providers.Event
	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, nil, func(ev providers.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}

	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[0].Content != "hi" {
		t.Fatalf("messages = %+v", msgs)
	}
	assistant := msgs[1]
	if assistant.Role != "assistant" || assistant.Content != "Hello" || assistant.Status != "complete" {
		t.Fatalf("assistant message = %+v", assistant)
	}

	gens, err := st.ListGenerationsForMessage(context.Background(), assistant.ID)
	if err != nil {
		t.Fatalf("list generations: %v", err)
	}
	if len(gens) != 1 {
		t.Fatalf("len(gens) = %d, want 1", len(gens))
	}
	g := gens[0]
	if g.ModelRequested != "requested-model" || g.ModelReported != "reported-model" {
		t.Fatalf("model requested/reported = %q/%q", g.ModelRequested, g.ModelReported)
	}
	if g.FirstTokenAt == nil || g.FinishedAt == nil || g.FinishReason != "stop" {
		t.Fatalf("generation = %+v", g)
	}
	if g.UsageJSON == "{}" || g.TimingsJSON == "{}" {
		t.Fatalf("usage/timings not recorded: %+v", g)
	}
}

// Regression: persistence writes must not share one context with a
// deadline fixed when Run started. A single persistCtx with, say, a
// 5-second timeout created once at the top of Run would already have
// expired by the time a generation slower than that reaches its final
// write, silently leaving the message stuck at status "streaming"
// forever even though the request itself completed successfully. This
// was caught against the real Strata server during the step 6 manual
// walkthrough, where a generation that took about 12 seconds left its
// assistant message empty and still marked "streaming" in the
// database despite the HTTP request itself finishing with 200 OK.
func TestRunner_SlowGenerationStillPersistsFinalState(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{
		ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`,
		// Longer than any single fixed deadline this step's bug used.
		ChatFirstByteDelay: 5500 * time.Millisecond,
	})
	defer srv.Close()

	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")

	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, nil, func(providers.Event) error { return nil })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	assistant := msgs[1]
	if assistant.Status != "complete" || assistant.Content != "Hello" {
		t.Fatalf("assistant message = %+v, want status complete with its content persisted", assistant)
	}
}

// A-C4: an explicit cancel through Registry aborts the upstream
// request and keeps the partial text, marked "cancelled".
func TestRunner_ExplicitCancelMarksCancelled(t *testing.T) {
	// A first-byte delay gives the test time to call Cancel before the
	// stream would otherwise complete on its own.
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`, ChatFirstByteDelay: 300 * time.Millisecond})
	defer srv.Close()

	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")

	// onStarted hands us the generation ID as soon as it exists; cancel
	// it shortly after, once Register has had time to run, so this
	// exercises a genuine mid-flight cancel rather than one raced
	// against the registration itself.
	_ = rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, func(generationID int64) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			rn.Registry.Cancel(generationID)
		}()
	}, func(providers.Event) error { return nil })

	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Status != "cancelled" {
		t.Fatalf("assistant message = %+v, want status cancelled", msgs[1])
	}
}

// A-C5: the caller's context being cancelled (a browser disconnect,
// not an explicit stop) marks the message "interrupted" rather than
// "cancelled".
func TestRunner_ContextCancelMarksInterrupted(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`, ChatFirstByteDelay: 300 * time.Millisecond})
	defer srv.Close()

	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "hi"}, nil, func(providers.Event) error { return nil })

	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[1].Status != "interrupted" {
		t.Fatalf("assistant message = %+v, want status interrupted", msgs[1])
	}
}

// S15: a model cached as not-loaded refuses the request outright
// (nothing persisted) unless the caller sets ConfirmLoad.
func TestRunner_RefusesNotLoadedModelWithoutConfirmation(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srv.Close()

	rn, st := newRunner(t)
	ep, conv := mustEndpointAndConversation(t, st, srv.URL, "m")
	if err := st.UpsertModel(context.Background(), store.Model{EndpointID: ep.ID, ModelID: "m", Type: "llm", State: "not-loaded", SeenAt: time.Now()}); err != nil {
		t.Fatalf("upsert model: %v", err)
	}

	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, nil, func(providers.Event) error { return nil })
	if err != ErrModelNotLoaded {
		t.Fatalf("err = %v, want ErrModelNotLoaded", err)
	}
	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("messages = %+v, want none (request not sent)", msgs)
	}

	// With confirmation, the same request goes through.
	err = rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi", ConfirmLoad: true}, nil, func(providers.Event) error { return nil })
	if err != nil {
		t.Fatalf("Run with ConfirmLoad: %v", err)
	}
}

// Switching a conversation's model mid-stream-of-turns must not lose
// any earlier message.
func TestRunner_ModelSwitchKeepsHistory(t *testing.T) {
	srvA := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srvA.Close()
	srvB := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srvB.Close()

	rn, st := newRunner(t)
	epA, conv := mustEndpointAndConversation(t, st, srvA.URL, "model-a")

	if err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "first"}, nil, func(providers.Event) error { return nil }); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	epB, err := st.CreateEndpoint(context.Background(), "b", "openai", srvB.URL, "local", 1)
	if err != nil {
		t.Fatalf("create endpoint b: %v", err)
	}
	modelB := "model-b"
	if err := st.SetConversationModel(context.Background(), conv.ID, &epB.ID, &modelB); err != nil {
		t.Fatalf("switch model: %v", err)
	}

	if err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "second"}, nil, func(providers.Event) error { return nil }); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	msgs, err := st.ListMessages(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("len(msgs) = %d, want 4 (two turns kept across the switch)", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[2].Content != "second" {
		t.Fatalf("messages = %+v", msgs)
	}
	_ = epA
}
