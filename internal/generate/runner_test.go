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
	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, func(ev providers.Event) error {
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

// A-C4: an explicit cancel through Registry aborts the upstream
// request and keeps the partial text, marked "cancelled".
func TestRunner_ExplicitCancelMarksCancelled(t *testing.T) {
	// A first-byte delay gives the test time to call Cancel before the
	// stream would otherwise complete on its own.
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: runnerTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`, ChatFirstByteDelay: 300 * time.Millisecond})
	defer srv.Close()

	rn, st := newRunner(t)
	_, conv := mustEndpointAndConversation(t, st, srv.URL, "m")

	// Find the generation ID as soon as it is created, from another
	// goroutine, so we can cancel it mid-flight.
	go func() {
		time.Sleep(50 * time.Millisecond)
		msgs, err := st.ListMessages(context.Background(), conv.ID)
		if err != nil || len(msgs) < 2 {
			return
		}
		gens, err := st.ListGenerationsForMessage(context.Background(), msgs[1].ID)
		if err != nil || len(gens) == 0 {
			return
		}
		rn.Registry.Cancel(gens[0].ID)
	}()

	_ = rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, func(providers.Event) error { return nil })

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

	_ = rn.Run(ctx, Request{ConversationID: conv.ID, UserContent: "hi"}, func(providers.Event) error { return nil })

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

	err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi"}, func(providers.Event) error { return nil })
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
	err = rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "hi", ConfirmLoad: true}, func(providers.Event) error { return nil })
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

	if err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "first"}, func(providers.Event) error { return nil }); err != nil {
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

	if err := rn.Run(context.Background(), Request{ConversationID: conv.ID, UserContent: "second"}, func(providers.Event) error { return nil }); err != nil {
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
