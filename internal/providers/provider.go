// Package providers defines the one interface the backend talks to
// inference providers through (PROVIDER-SPEC.md section 1). Phase 1
// has a single implementation, internal/providers/openai, used for
// every endpoint regardless of detected service.
package providers

import (
	"context"
	"fmt"
)

// Model is a discovered model's capability profile (PROVIDER-SPEC.md
// section 2). Model identity is always the pair (endpoint ID, Model.ID)
// (ARCHITECTURE.md section 5); this struct carries only the model side
// of that pair.
type Model struct {
	ID             string
	Type           string // "llm", "vlm" or "embeddings"; defaults to "llm"
	Chat           bool
	Streaming      bool
	Embeddings     bool
	Vision         bool
	Tools          bool
	ReasoningField string // e.g. "reasoning_content"; empty if none observed
	ContextLength  int
	State          string // "loaded" or "not-loaded"
	Concurrency    int
}

// Detection is the result of Provider.Detect.
type Detection struct {
	DetectedService string // "strata", "lm-studio" or "openai-compatible"
	Healthy         bool
}

// Message is one chat turn in a ChatRequest.
type Message struct {
	Role    string // "system", "user" or "assistant"
	Content string
}

// ChatRequest is a normalized chat request (PROVIDER-SPEC.md section
// 1.2). ReasoningEffort is Strata-only and is dropped by adapters for
// any other detected service.
type ChatRequest struct {
	Model           string
	Messages        []Message
	MaxTokens       int
	Temperature     *float64
	TopP            *float64
	Stop            []string
	ReasoningEffort string
}

// EventKind distinguishes normalized stream events (PROVIDER-SPEC.md
// section 1.1).
type EventKind string

const (
	EventStart   EventKind = "start"
	EventDelta   EventKind = "delta"
	EventUsage   EventKind = "usage"
	EventTimings EventKind = "timings"
	EventDone    EventKind = "done"
)

// Event is one normalized stream event. Only the fields relevant to
// Kind are meaningful.
type Event struct {
	Kind EventKind

	// EventDelta
	Content   string
	Reasoning string

	// EventUsage
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int

	// EventTimings
	TTFTMillis      float64
	TokensPerSecond float64

	// EventDone
	FinishReason  string
	ModelReported string
}

// Result is returned when StreamChat finishes, successfully or not.
// Partial results (a model ID seen before an error, a finish reason
// from a done event) are still reported, since the caller persists
// whatever text streamed before a failure.
type Result struct {
	ModelReported string
	FinishReason  string
}

// ErrorKind is the normalized error taxonomy (PROVIDER-SPEC.md section
// 1.3).
type ErrorKind string

const (
	ErrUnreachable     ErrorKind = "unreachable"
	ErrTimeout         ErrorKind = "timeout"
	ErrAuth            ErrorKind = "auth"
	ErrModelNotFound   ErrorKind = "model_not_found"
	ErrNoModelLoaded   ErrorKind = "no_model_loaded"
	ErrContextOverflow ErrorKind = "context_overflow"
	ErrBusy            ErrorKind = "busy"
	ErrUpstreamError   ErrorKind = "upstream_error"
	ErrProtocolError   ErrorKind = "protocol_error"
	ErrCancelled       ErrorKind = "cancelled"
)

// Error carries a normalized ErrorKind alongside the underlying cause,
// so API handlers can report the UI meaning from PROVIDER-SPEC.md
// section 1.3 without string-matching error text.
type Error struct {
	Kind    ErrorKind
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Provider is the one interface the backend talks to providers through
// (PROVIDER-SPEC.md section 1). Cancelling ctx aborts the upstream HTTP
// request; there is no separate cancel call.
type Provider interface {
	Detect(ctx context.Context) (Detection, error)
	ListModels(ctx context.Context) ([]Model, error)
	StreamChat(ctx context.Context, req ChatRequest, emit func(Event) error) (Result, error)
}
