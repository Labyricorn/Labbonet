package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Generate holds the dependencies for the generation-stream and cancel
// handlers (ARCHITECTURE.md section 4.1, 4.2). Generations run as detached jobs: the
// POST that starts one and any later GET .../events only watch it.
type Generate struct {
	Runner *generate.Runner
	// Jobs may be left nil; it is then built around Runner on first use.
	Jobs *generate.Jobs

	once sync.Once
}

func (g *Generate) jobs() *generate.Jobs {
	g.once.Do(func() {
		if g.Jobs == nil {
			g.Jobs = generate.NewJobs(g.Runner)
		}
	})
	return g.Jobs
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
	// Reply delivery limits and text attachments (see generate.Request).
	MaxReplyChars       int                   `json:"max_reply_chars"`
	ReasoningLimitChars int                   `json:"reasoning_limit_chars"`
	Attachments         []generate.Attachment `json:"attachments"`
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
	// Status and ErrorKind are set on the final "end" event.
	Status    string `json:"status,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
}

// writeSSEUpdate writes one job update as an SSE "data:" line. generationID is on every
// event so the client's Stop button always has it on hand.
func writeSSEUpdate(w http.ResponseWriter, flusher http.Flusher, u generate.Update) error {
	payload := sseEventPayload{Kind: u.Kind, GenerationID: u.GenerationID}
	switch u.Kind {
	case "snapshot":
		payload.Content, payload.Reasoning = u.Content, u.Reasoning
	case "end":
		payload.Status, payload.ErrorKind, payload.FinishReason = u.Status, u.ErrorKind, u.FinishReason
	default:
		ev := u.Event
		payload.Kind = string(ev.Kind)
		payload.Content, payload.Reasoning = ev.Content, ev.Reasoning
		payload.PromptTokens, payload.CompletionTokens, payload.CachedTokens = ev.PromptTokens, ev.CompletionTokens, ev.CachedTokens
		payload.TTFTMillis, payload.TokensPerSecond = ev.TTFTMillis, ev.TokensPerSecond
		payload.FinishReason, payload.ModelReported = ev.FinishReason, ev.ModelReported
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// relay copies a job's updates to the response until the job ends or the reader goes
// away. Leaving never stops the job.
func relay(w http.ResponseWriter, r *http.Request, flusher http.Flusher, job *generate.Job) {
	sub := job.Subscribe()
	defer sub.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case u, ok := <-sub.C:
			if !ok {
				return
			}
			if err := writeSSEUpdate(w, flusher, u); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// Generate handles POST /api/conversations/{id}/generate: appends the user message,
// starts the reply as a detached job and streams it over SSE (ARCHITECTURE.md section
// 4.2). A failure before the job starts reports as an ordinary HTTP error status. Once
// it has started, the stream is only a view: if this request ends the reply continues
// and can be re-attached with GET /api/generations/{id}/events.
func (g *Generate) Generate(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2*generate.MaxAttachmentTotal+(1<<20))
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
		MaxReplyChars: req.MaxReplyChars, ReasoningLimitChars: req.ReasoningLimitChars, Attachments: req.Attachments,
	}
	job, err := g.jobs().Start(genReq)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			http.Error(w, "Conversation changed or is generating; reload before sending", http.StatusConflict)
		case errors.Is(err, generate.ErrPrompt), errors.Is(err, generate.ErrAttachment), errors.Is(err, generate.ErrLimits):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, generate.ErrModelNotLoaded), errors.Is(err, generate.ErrNoModelSelected):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	relay(w, r, flusher, job)
}

// Events handles GET /api/generations/{id}/events: re-attaches to a running (or just
// finished) generation. The first event is a snapshot of the text so far. A generation
// that is no longer held in memory is 404; its stored message is then the record.
func (g *Generate) Events(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	job := g.jobs().Get(id)
	if job == nil {
		http.Error(w, "generation is not running", http.StatusNotFound)
		return
	}
	relay(w, r, flusher, job)
}

type activeResponse struct {
	GenerationID int64 `json:"generation_id"`
	Active       bool  `json:"active"`
}

// Active handles GET /api/conversations/{id}/generation: the conversation's running
// generation, if any, so a reloaded page can re-attach.
func (g *Generate) Active(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if job := g.jobs().Active(id); job != nil && job.Running() {
		writeJSON(w, http.StatusOK, activeResponse{GenerationID: job.GenerationID, Active: true})
		return
	}
	writeJSON(w, http.StatusOK, activeResponse{})
}

type cancelResponse struct {
	Cancelled bool `json:"cancelled"`
}

// Cancel handles POST /api/generations/{id}/cancel (A-C4): aborts the
// upstream request for a running generation. Partial text already
// streamed is kept by Runner, marked "cancelled". A generation still waiting for its
// endpoint's slot is cancelled the same way.
func (g *Generate) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cancelled := g.Runner.Registry.Cancel(id)
	writeJSON(w, http.StatusOK, cancelResponse{Cancelled: cancelled})
}
