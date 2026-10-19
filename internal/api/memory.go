package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/memory"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Memory serves the lore/summary/retrieval settings, the per-conversation summary and
// the retrieval inspector. Nothing here sends a request to a provider except the explicit
// "summarise now" action.
type Memory struct {
	Store  *store.Store
	Runner *generate.Runner
}

func memoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "conversation not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "The summary or settings changed in another tab or window; reload before saving", http.StatusConflict)
	case errors.Is(err, store.ErrBusy), errors.Is(err, generate.ErrNoModelSelected), errors.Is(err, generate.ErrEndpointDisabled):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, store.ErrInvalidMemory), memory.IsSettingError(err), errors.Is(err, generate.ErrNothingToSummarise):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusInternalServerError)
	}
}

// GetSettings handles GET /api/memory/settings.
func (m *Memory) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := m.Store.GetMemorySettings(r.Context())
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// PutSettings handles PUT /api/memory/settings; the body carries the revision it edited.
func (m *Memory) PutSettings(w http.ResponseWriter, r *http.Request) {
	var in memory.Settings
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Revision <= 0 {
		http.Error(w, "revision is required", http.StatusBadRequest)
		return
	}
	s, err := m.Store.SaveMemorySettings(r.Context(), in)
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

type entityPreview struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Tags      int    `json:"tags"`
	Relations int    `json:"relations"`
	Offscreen bool   `json:"offscreen"`
	Chars     int    `json:"chars"`
}

type storybookPreview struct {
	Counts   memory.Counts   `json:"counts"`
	Entities []entityPreview `json:"entities"`
	Chunks   int             `json:"prose_chunks"`
	Docs     int             `json:"embeddable_docs"`
	Warnings []string        `json:"warnings"`
}

// PreviewStorybook handles POST /api/memory/storybook/preview. It parses only; it never
// stores anything and never calls a model.
func (m *Memory) PreviewStorybook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Storybook string `json:"storybook"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, memory.MaxStorybookBytes+4096)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	book, err := memory.ParseStorybook(in.Storybook)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := storybookPreview{Counts: book.Counts, Entities: []entityPreview{}, Chunks: len(book.ProseChunks), Docs: len(book.Docs()), Warnings: book.Warnings}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	for _, id := range book.Order {
		e := book.Entities[id]
		out.Entities = append(out.Entities, entityPreview{ID: id, Kind: e.Kind, Tags: len(e.Tags), Relations: len(e.Relations), Offscreen: e.OffscreenDefault, Chars: len(e.Prose)})
	}
	writeJSON(w, http.StatusOK, out)
}

// EmbeddingStats handles GET /api/memory/embeddings.
func (m *Memory) EmbeddingStats(w http.ResponseWriter, r *http.Request) {
	stats, err := m.Store.EmbeddingCacheStats(r.Context())
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// ClearEmbeddings handles DELETE /api/memory/embeddings.
func (m *Memory) ClearEmbeddings(w http.ResponseWriter, r *http.Request) {
	n, err := m.Store.ClearEmbeddingCache(r.Context())
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"cleared": n})
}

type memoryResponse struct {
	Summary  *store.Summary          `json:"summary"`
	History  []store.Summary         `json:"history"`
	Records  []store.RetrievalRecord `json:"records"`
	Archived int                     `json:"archived_messages"`
	Live     int                     `json:"live_messages"`
	Seq      int64                   `json:"latest_seq"`
}

// activeSummary resolves the effective summary for a conversation's visible branch.
func (m *Memory) activeSummary(r *http.Request, convID int64) (*store.Summary, []store.PathMessage, int, error) {
	path, _, err := m.Store.ActivePath(r.Context(), convID)
	if err != nil {
		return nil, nil, 0, err
	}
	onPath := make(map[int64]int, len(path))
	for i, p := range path {
		onPath[p.ID] = i
	}
	eff, err := m.Store.EffectiveSummary(r.Context(), convID, onPath)
	if err != nil {
		return nil, nil, 0, err
	}
	boundary := 0
	if eff != nil && eff.ThroughMessageID != nil {
		boundary = onPath[*eff.ThroughMessageID] + 1
	}
	return eff, path, boundary, nil
}

// Conversation handles GET /api/conversations/{id}/memory: the summary in force on the
// visible branch, its revision history (including failed passes) and recent retrieval
// records. Records hold identifiers and scores only.
func (m *Memory) Conversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err = m.Store.GetConversation(r.Context(), id); err != nil {
		memoryError(w, err)
		return
	}
	eff, path, boundary, err := m.activeSummary(r, id)
	if err != nil {
		memoryError(w, err)
		return
	}
	out := memoryResponse{Summary: eff, Archived: boundary, Live: len(path) - boundary}
	if out.History, err = m.Store.ListSummaries(r.Context(), id, 50); err == nil {
		out.Seq, err = m.Store.LatestSummarySeq(r.Context(), id)
	}
	if err == nil {
		out.Records, err = m.Store.ListRetrievalRecords(r.Context(), id, 20)
	}
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type summaryEdit struct {
	Text        string `json:"text"`
	ExpectedSeq int64  `json:"expected_seq"`
}

// EditSummary handles PUT /api/conversations/{id}/summary: a hand edit becomes a new
// revision with the same archive boundary. Empty text clears the summary and returns the
// archived messages to the prompt. ExpectedSeq is the latest revision the editor saw.
func (m *Memory) EditSummary(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in summaryEdit
	if !decodeBody(w, r, &in) {
		return
	}
	eff, path, _, err := m.activeSummary(r, id)
	if err != nil {
		memoryError(w, err)
		return
	}
	// Branch scope (anchor) and archive coverage (through/count) are independent: an edit keeps
	// the coverage of the summary it replaces but is visible only where its anchor is.
	row := store.Summary{ConversationID: id, Text: in.Text, Source: "manual", AnchorMessageID: store.ManualAnchor(path)}
	if in.Text != "" && eff != nil {
		row.ThroughMessageID, row.ArchivedCount = eff.ThroughMessageID, eff.ArchivedCount
	}
	saved, err := m.Store.AddSummary(r.Context(), row, in.ExpectedSeq)
	if err != nil {
		memoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

type summariseResponse struct {
	Summary *store.Summary `json:"summary"`
	Notes   []string       `json:"notes"`
}

// SummariseNow handles POST /api/conversations/{id}/summary/compress: an explicit,
// user-triggered summary pass that calls the compression (or chat) model once.
func (m *Memory) SummariseNow(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	row, notes, err := m.Runner.SummarizeNow(ctx, id)
	if err != nil {
		memoryError(w, err)
		return
	}
	if notes == nil {
		notes = []string{}
	}
	writeJSON(w, http.StatusOK, summariseResponse{Summary: row, Notes: notes})
}
