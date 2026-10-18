package generate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/providers/openai"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// ErrNoModelSelected is returned when the conversation has no
// endpoint/model chosen yet.
var ErrNoModelSelected = errors.New("conversation has no endpoint or model selected")

// ErrModelNotLoaded is returned when the target model's cached state
// is not-loaded and the request did not set ConfirmLoad
// (SECURITY-MODEL.md S15, PROVIDER-SPEC.md section 6).
var ErrModelNotLoaded = errors.New("model is not loaded; resend with confirm_load to allow it to load, which may evict another client's model")

// Request is what the API layer gathers before calling Run.
type Request struct {
	ConversationID  int64
	UserContent     string
	MaxTokens       int
	Temperature     *float64
	TopP            *float64
	Stop            []string
	ReasoningEffort string
	ConfirmLoad     bool
}

// Runner drives one generation end to end (ARCHITECTURE.md section
// 4.2).
type Runner struct {
	Store     *store.Store
	Protector secrets.Protector
	Egress    *egress.Client
	Slots     *Slots
	Registry  *Registry
}

// Run persists the user message, creates the assistant message and its
// generation row, acquires the endpoint's concurrency slot, streams
// the reply, and records its terminal status. Every normalized event
// is also passed to emit (the caller relays these over SSE). Partial
// text is always kept, no matter how the stream ends (success, an
// explicit cancel, a browser disconnect, or an upstream error).
//
// Cancelling ctx (the browser disconnecting) marks the message
// "interrupted". A generation registered here and then aborted through
// Registry.Cancel instead marks it "cancelled" (A-C4 vs. A-C5).
func (rn *Runner) Run(ctx context.Context, req Request, emit func(providers.Event) error) error {
	// Persistence during and after the stream must survive ctx being
	// cancelled (a browser disconnect or an explicit stop both cancel
	// ctx or a context derived from it): "Partial text is always kept"
	// requires writing it with a context that isn't itself the thing
	// that just got cancelled.
	persistCtx, cancelPersist := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPersist()

	conv, err := rn.Store.GetConversation(ctx, req.ConversationID)
	if err != nil {
		return fmt.Errorf("conversation not found: %w", err)
	}
	if conv.EndpointID == nil || conv.ModelID == nil {
		return ErrNoModelSelected
	}
	endpointID := *conv.EndpointID
	modelID := *conv.ModelID

	ep, err := rn.Store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return fmt.Errorf("endpoint not found: %w", err)
	}

	if !req.ConfirmLoad {
		if err := rn.refuseIfNotLoaded(ctx, endpointID, modelID); err != nil {
			return err
		}
	}

	if _, err := rn.Store.CreateMessage(ctx, req.ConversationID, nil, "user", req.UserContent, "", "complete"); err != nil {
		return fmt.Errorf("persist user message: %w", err)
	}

	history, err := rn.buildHistory(ctx, req.ConversationID, conv.SystemPrompt)
	if err != nil {
		return fmt.Errorf("build history: %w", err)
	}

	assistantMsg, err := rn.Store.CreateMessage(ctx, req.ConversationID, nil, "assistant", "", "", "streaming")
	if err != nil {
		return fmt.Errorf("create assistant message: %w", err)
	}

	paramsJSON, _ := json.Marshal(req)
	gen, err := rn.Store.CreateGeneration(ctx, assistantMsg.ID, &endpointID, modelID, string(paramsJSON))
	if err != nil {
		return fmt.Errorf("create generation: %w", err)
	}

	target, err := rn.targetFor(ctx, ep)
	if err != nil {
		rn.recordFailure(persistCtx, assistantMsg.ID, gen.ID, err)
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	rn.Registry.Register(gen.ID, cancel)
	defer func() {
		cancel()
		rn.Registry.Unregister(gen.ID)
	}()

	release, err := rn.Slots.Acquire(runCtx, endpointID, ep.ConcurrencyLimit)
	if err != nil {
		rn.recordFailure(persistCtx, assistantMsg.ID, gen.ID, err)
		return err
	}
	defer release()

	adapter := openai.New(rn.Egress, target)
	chatReq := providers.ChatRequest{
		Model: modelID, Messages: history, MaxTokens: req.MaxTokens,
		Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop,
		ReasoningEffort: req.ReasoningEffort,
	}

	acc := newAccumulator()
	lastFlush := time.Now()

	result, streamErr := adapter.StreamChat(runCtx, chatReq, func(ev providers.Event) error {
		switch ev.Kind {
		case providers.EventDelta:
			acc.add(ev)
			if !acc.firstTokenRecorded {
				acc.firstTokenRecorded = true
				_ = rn.Store.RecordFirstToken(persistCtx, gen.ID, "")
			}
			if time.Since(lastFlush) >= time.Second {
				lastFlush = time.Now()
				if err := rn.Store.UpdateMessageContent(persistCtx, assistantMsg.ID, acc.content.String(), acc.reasoning.String(), "streaming"); err != nil {
					return err
				}
			}
		case providers.EventUsage:
			acc.usage = ev
		case providers.EventTimings:
			acc.timings = ev
		case providers.EventDone:
			if ev.ModelReported != "" {
				_ = rn.Store.SetModelReported(persistCtx, gen.ID, ev.ModelReported)
			}
		}
		return emit(ev)
	})

	// ARCHITECTURE.md section 4.2: "Partial text is always kept."
	finalStatus, errorKind := rn.finalStatus(gen.ID, streamErr)
	_ = rn.Store.UpdateMessageContent(persistCtx, assistantMsg.ID, acc.content.String(), acc.reasoning.String(), finalStatus)

	usageJSON, _ := json.Marshal(acc.usageJSON())
	timingsJSON, _ := json.Marshal(acc.timingsJSON())
	_ = rn.Store.FinishGeneration(persistCtx, gen.ID, result.FinishReason, string(usageJSON), string(timingsJSON), errorKind)

	return streamErr
}

// refuseIfNotLoaded implements the server-side S15 guard: the cached
// inventory is Phase 1's only signal of load state, so a stale or
// never-refreshed cache simply has no opinion (the request proceeds)
// rather than blocking on missing information.
func (rn *Runner) refuseIfNotLoaded(ctx context.Context, endpointID int64, modelID string) error {
	models, err := rn.Store.ListModels(ctx, endpointID)
	if err != nil {
		return nil
	}
	for _, m := range models {
		if m.ModelID == modelID && m.State == "not-loaded" {
			return ErrModelNotLoaded
		}
	}
	return nil
}

func (rn *Runner) buildHistory(ctx context.Context, conversationID int64, systemPrompt string) ([]providers.Message, error) {
	msgs, err := rn.Store.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	history := make([]providers.Message, 0, len(msgs)+1)
	if systemPrompt != "" {
		history = append(history, providers.Message{Role: "system", Content: systemPrompt})
	}
	for _, m := range msgs {
		history = append(history, providers.Message{Role: m.Role, Content: m.Content})
	}
	return history, nil
}

func (rn *Runner) targetFor(ctx context.Context, ep *store.Endpoint) (egress.Target, error) {
	u, err := url.Parse(ep.BaseURL)
	if err != nil {
		return egress.Target{}, fmt.Errorf("parse stored base URL: %w", err)
	}
	target := egress.Target{BaseURL: u, PrivacyClass: ep.PrivacyClass}

	hasCred, err := rn.Store.HasCredential(ctx, ep.ID)
	if err != nil {
		return egress.Target{}, err
	}
	if hasCred {
		blob, err := rn.Store.CredentialBlob(ctx, ep.ID)
		if err != nil {
			return egress.Target{}, err
		}
		value, err := rn.Protector.Unprotect(blob)
		if err != nil {
			return egress.Target{}, fmt.Errorf("unprotect credential: %w", err)
		}
		target.Credential = value
	}
	return target, nil
}

// recordFailure finalizes a generation that never got as far as
// streaming (target resolution or slot acquisition failed). persistCtx
// must not be one that err's own cancellation already cancelled.
func (rn *Runner) recordFailure(persistCtx context.Context, messageID, generationID int64, err error) {
	status, errorKind := rn.finalStatus(generationID, err)
	_ = rn.Store.UpdateMessageContent(persistCtx, messageID, "", "", status)
	_ = rn.Store.FinishGeneration(persistCtx, generationID, "", "{}", "{}", errorKind)
}

// finalStatus maps a stream result to a message status and, for
// errors, the error_kind to record. An explicit cancel (Registry) is
// "cancelled"; any other context cancellation is "interrupted" (a
// browser disconnect, ARCHITECTURE.md section 4.2); anything else is
// "error". It judges this from err alone (never from a live ctx, which
// may by now be the very thing that got cancelled).
func (rn *Runner) finalStatus(generationID int64, err error) (status, errorKind string) {
	if err == nil {
		return "complete", ""
	}
	var pe *providers.Error
	if errors.As(err, &pe) {
		if pe.Kind == providers.ErrCancelled {
			if rn.Registry.WasCancelled(generationID) {
				return "cancelled", ""
			}
			return "interrupted", ""
		}
		return "error", string(pe.Kind)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		if rn.Registry.WasCancelled(generationID) {
			return "cancelled", ""
		}
		return "interrupted", ""
	}
	return "error", "upstream_error"
}

type accumulator struct {
	content            strings.Builder
	reasoning          strings.Builder
	firstTokenRecorded bool
	usage              providers.Event
	timings            providers.Event
}

func newAccumulator() *accumulator {
	return &accumulator{}
}

func (a *accumulator) add(ev providers.Event) {
	a.content.WriteString(ev.Content)
	a.reasoning.WriteString(ev.Reasoning)
}

type usageJSON struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

type timingsJSON struct {
	TTFTMillis      float64 `json:"ttft_ms"`
	TokensPerSecond float64 `json:"tokens_per_second"`
}

func (a *accumulator) usageJSON() usageJSON {
	return usageJSON{
		PromptTokens:     a.usage.PromptTokens,
		CompletionTokens: a.usage.CompletionTokens,
		CachedTokens:     a.usage.CachedTokens,
	}
}

func (a *accumulator) timingsJSON() timingsJSON {
	return timingsJSON{TTFTMillis: a.timings.TTFTMillis, TokensPerSecond: a.timings.TokensPerSecond}
}
