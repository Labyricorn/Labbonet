package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Generate holds the dependencies for the generation-stream and cancel
// handlers (ARCHITECTURE.md section 4.1, 4.2).
type Generate struct {
	Runner *generate.Runner
}

type generateRequest struct {
	Content         string   `json:"content"`
	MaxTokens       int      `json:"max_tokens"`
	Temperature     *float64 `json:"temperature"`
	TopP            *float64 `json:"top_p"`
	Stop            []string `json:"stop"`
	ReasoningEffort string   `json:"reasoning_effort"`
	// ConfirmLoad must be set to send to a model whose cached state is
	// not-loaded (SECURITY-MODEL.md S15); the server enforces this, the
	// UI cannot skip it by omitting a client-side check.
	ConfirmLoad bool `json:"confirm_load"`
	// RegenerateParentID asks for a new reply variant to that user message instead of a
	// new user turn; Revision, when set, is the conversation revision the client saw.
	RegenerateParentID *int64 `json:"regenerate_parent_id"`
	Revision           int64  `json:"revision"`
}

type sseEventPayload struct {
	Kind             string  `json:"kind"`
	GenerationID     int64   `json:"generation_id,omitempty"`
	Content          string  `json:"content,omitempty"`
	Reasoning        string  `json:"reasoning,omitempty"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	TTFTMillis       float64 `json:"ttft_ms,omitempty"`
	TokensPerSecond  float64 `json:"tokens_per_second,omitempty"`
	FinishReason     string  `json:"finish_reason,omitempty"`
	ModelReported    string  `json:"model_reported,omitempty"`
}

// writeSSEEvent writes one normalized event as an SSE "data:" line.
// generationID is included on every event (not just the first), so the
// client's Stop button always has it on hand to call
// POST /api/generations/{id}/cancel, even if it only kept the latest
// event.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, generationID int64, ev providers.Event) error {
	payload := sseEventPayload{
		Kind: string(ev.Kind), GenerationID: generationID, Content: ev.Content, Reasoning: ev.Reasoning,
		PromptTokens: ev.PromptTokens, CompletionTokens: ev.CompletionTokens, CachedTokens: ev.CachedTokens,
		TTFTMillis: ev.TTFTMillis, TokensPerSecond: ev.TokensPerSecond,
		FinishReason: ev.FinishReason, ModelReported: ev.ModelReported,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// Generate handles POST /api/conversations/{id}/generate: appends the
// user message and streams the reply over SSE (ARCHITECTURE.md section
// 4.2). A failure that happens before any event is written reports as
// an ordinary HTTP error status; a failure mid-stream has already
// committed to the SSE content type, so the stream simply ends (the
// generations row records the error_kind; the client sees the
// connection close).
func (g *Generate) Generate(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" && req.RegenerateParentID == nil {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	genReq := generate.Request{
		ConversationID: id, UserContent: req.Content, MaxTokens: req.MaxTokens,
		Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop,
		ReasoningEffort: req.ReasoningEffort, ConfirmLoad: req.ConfirmLoad,
		RegenerateParentID: req.RegenerateParentID, ExpectedRevision: req.Revision,
	}

	var genID int64
	headersSent := false
	runErr := g.Runner.Run(r.Context(), genReq, func(generationID int64) {
		genID = generationID
	}, func(ev providers.Event) error {
		if !headersSent {
			headersSent = true
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
		}
		return writeSSEEvent(w, flusher, genID, ev)
	})

	if runErr == nil || headersSent {
		return
	}
	switch {
	case errors.Is(runErr, store.ErrConflict):
		http.Error(w, "Conversation changed or is generating; reload before sending", http.StatusConflict)
	case errors.Is(runErr, generate.ErrPrompt):
		http.Error(w, runErr.Error(), http.StatusBadRequest)
	case errors.Is(runErr, generate.ErrModelNotLoaded):
		http.Error(w, runErr.Error(), http.StatusConflict)
	case errors.Is(runErr, generate.ErrNoModelSelected):
		http.Error(w, runErr.Error(), http.StatusConflict)
	default:
		http.Error(w, runErr.Error(), http.StatusBadGateway)
	}
}

type cancelResponse struct {
	Cancelled bool `json:"cancelled"`
}

// Cancel handles POST /api/generations/{id}/cancel (A-C4): aborts the
// upstream request for a running generation. Partial text already
// streamed is kept by Runner, marked "cancelled".
func (g *Generate) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cancelled := g.Runner.Registry.Cancel(id)
	writeJSON(w, http.StatusOK, cancelResponse{Cancelled: cancelled})
}
