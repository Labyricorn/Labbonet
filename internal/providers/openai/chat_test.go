package openai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
)

// Built verbatim from the observed Strata example in
// Strata_Protocol.md sections 5.3 and 5.4: a keep-alive comment, an
// empty-content role chunk, two content deltas, a final chunk carrying
// finish_reason/usage/timings together, then "[DONE]".
const strataSSE = ": keep-alive\n\n" +
	`data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}],"model":"qwen3.8-flash-next-unsloth-ud-iq4_xs"}` + "\n\n" +
	`data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n" +
	`data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":" world"},"finish_reason":null}]}` + "\n\n" +
	`data: {"object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}],"model":"qwen3.8-flash-next-unsloth-ud-iq4_xs","usage":{"prompt_tokens":18,"completion_tokens":4,"total_tokens":22,"prompt_tokens_details":{"cached_tokens":0}},"timings":{"prompt_n":18,"prompt_ms":630.1,"prompt_per_second":28.6,"predicted_n":4,"predicted_ms":393.9,"predicted_per_second":10.2,"cache_n":0,"draft_n":0,"draft_n_accepted":0}}` + "\n\n" +
	"data: [DONE]\n\n"

func chatReq() providers.ChatRequest {
	return providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "hi"}}}
}

func TestStreamChat_StrataExample(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: strataSSE})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileStrata

	var events []providers.Event
	result, err := a.StreamChat(context.Background(), providers.ChatRequest{
		Model:    "nope", // Strata ignores the requested model id
		Messages: []providers.Message{{Role: "user", Content: "hi"}},
	}, func(ev providers.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if result.FinishReason != "stop" || result.ModelReported != "qwen3.8-flash-next-unsloth-ud-iq4_xs" {
		t.Fatalf("result = %+v", result)
	}

	var content strings.Builder
	var sawUsage, sawTimings, sawStart bool
	for _, ev := range events {
		switch ev.Kind {
		case providers.EventStart:
			sawStart = true
		case providers.EventDelta:
			content.WriteString(ev.Content)
		case providers.EventUsage:
			sawUsage = true
			if ev.PromptTokens != 18 || ev.CompletionTokens != 4 {
				t.Fatalf("usage event = %+v", ev)
			}
		case providers.EventTimings:
			sawTimings = true
			if ev.TokensPerSecond != 10.2 {
				t.Fatalf("timings event = %+v", ev)
			}
		}
	}
	if !sawStart || !sawUsage || !sawTimings {
		t.Fatalf("missing an expected event kind: start=%v usage=%v timings=%v", sawStart, sawUsage, sawTimings)
	}
	if content.String() != "Hello world" {
		t.Fatalf("content = %q, want %q", content.String(), "Hello world")
	}
}

// A null content with finish_reason "length" is a truncation, not an
// error (Strata_Protocol.md section 5.4).
func TestStreamChat_NullContentWithLengthIsNotAnError(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":null,"reasoning_content":"thinking..."},"finish_reason":"length"}]}` + "\n\n" + "data: [DONE]\n\n"
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: sse})
	defer srv.Close()

	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileStrata

	var sawReasoning bool
	result, err := a.StreamChat(context.Background(), chatReq(), func(ev providers.Event) error {
		if ev.Kind == providers.EventDelta && ev.Reasoning != "" {
			sawReasoning = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if result.FinishReason != "length" {
		t.Fatalf("FinishReason = %q, want length", result.FinishReason)
	}
	if !sawReasoning {
		t.Fatal("reasoning_content delta was not relayed")
	}
}

func TestStreamChat_ErrorStatus(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   providers.ErrorKind
	}{
		{"auth", 401, `{"error":{"message":"missing api key"}}`, providers.ErrAuth},
		{"no model loaded", 503, `{"error":{"message":"No models loaded. Please load a model."}}`, providers.ErrNoModelLoaded},
		{"busy", 409, `{"error":{"message":"busy"}}`, providers.ErrBusy},
		{"context overflow", 400, `{"error":{"message":"context window exceeded"}}`, providers.ErrContextOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeprovider.New(fakeprovider.Script{ChatStatus: c.status, ChatErrorBody: c.body})
			defer srv.Close()

			a := New(egress.NewClient(), targetFor(t, srv.URL))
			a.profile = ProfileGeneric
			_, err := a.StreamChat(context.Background(), chatReq(), func(providers.Event) error { return nil })
			var pe *providers.Error
			if !errors.As(err, &pe) || pe.Kind != c.want {
				t.Fatalf("err = %v, want kind %q", err, c.want)
			}
		})
	}
}

func TestStreamChat_CancelledContext(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: strataSSE})
	defer srv.Close()
	a := New(egress.NewClient(), targetFor(t, srv.URL))
	a.profile = ProfileGeneric

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.StreamChat(ctx, chatReq(), func(providers.Event) error { return nil })
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrCancelled {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestBuildChatBody_ReasoningEffortAllowlist(t *testing.T) {
	req := providers.ChatRequest{Messages: []providers.Message{{Role: "user", Content: "hi"}}, ReasoningEffort: "low"}

	strata := (&Adapter{profile: ProfileStrata}).buildChatBody(req)
	if strata.ReasoningEffort != "low" {
		t.Fatalf("strata ReasoningEffort = %q, want low", strata.ReasoningEffort)
	}

	lmStudio := (&Adapter{profile: ProfileLMStudio}).buildChatBody(req)
	if lmStudio.ReasoningEffort != "" {
		t.Fatalf("lm-studio ReasoningEffort = %q, want empty", lmStudio.ReasoningEffort)
	}
}

// The wire body has no field for strata_mcp or ttl at all
// (PROVIDER-SPEC.md section 1.2); this checks the actual marshaled
// JSON rather than relying only on that compile-time guarantee.
func TestBuildChatBody_NeverSendsStrataMCPOrTTL(t *testing.T) {
	a := &Adapter{profile: ProfileStrata}
	raw, err := json.Marshal(a.buildChatBody(chatReq()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "strata_mcp") || strings.Contains(s, `"ttl"`) {
		t.Fatalf("request body contains a forbidden field: %s", s)
	}
}
