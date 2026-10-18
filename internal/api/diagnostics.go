package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Labyricorn/Labbonet/internal/store"
)

// Diagnostics holds the dependencies for GET /api/diagnostics.
type Diagnostics struct {
	Store *store.Store
}

// diagnosticsEntry is request metadata only: endpoint, model, status
// and timings. Status comes from the associated message's own status
// column (never from message content, and every other field here
// comes from the generations table alone), so this handler cannot leak
// conversation text or a credential (SECURITY-MODEL.md section 7;
// A-D2).
type diagnosticsEntry struct {
	GenerationID   int64           `json:"generation_id"`
	EndpointID     *int64          `json:"endpoint_id"`
	ModelRequested string          `json:"model_requested"`
	ModelReported  string          `json:"model_reported"`
	Status         string          `json:"status"`
	FinishReason   string          `json:"finish_reason"`
	ErrorKind      string          `json:"error_kind"`
	StartedAt      time.Time       `json:"started_at"`
	FirstTokenAt   *time.Time      `json:"first_token_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
	DurationMillis *float64        `json:"duration_ms,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	Timings        json.RawMessage `json:"timings,omitempty"`
}

// List handles GET /api/diagnostics: the most recent requests, for the
// diagnostics panel (ARCHITECTURE.md section 4.1, A-D1).
func (d *Diagnostics) List(w http.ResponseWriter, r *http.Request) {
	gens, err := d.Store.ListRecentGenerations(r.Context(), 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]diagnosticsEntry, 0, len(gens))
	for _, g := range gens {
		// generations has no status column of its own (ARCHITECTURE.md
		// section 5): "complete" vs "cancelled" vs "interrupted" vs
		// "error" lives on the message it belongs to. Deriving status
		// from error_kind/finished_at alone would show "complete" for a
		// generation a user explicitly stopped, since Runner finishes
		// a cancelled generation with an empty error_kind.
		status := "running"
		if msg, err := d.Store.GetMessage(r.Context(), g.MessageID); err == nil {
			status = msg.Status
		}
		entry := diagnosticsEntry{
			GenerationID: g.ID, EndpointID: g.EndpointID, ModelRequested: g.ModelRequested,
			ModelReported: g.ModelReported, Status: status, FinishReason: g.FinishReason,
			ErrorKind: g.ErrorKind, StartedAt: g.StartedAt, FirstTokenAt: g.FirstTokenAt, FinishedAt: g.FinishedAt,
		}
		if g.FinishedAt != nil {
			ms := g.FinishedAt.Sub(g.StartedAt).Seconds() * 1000
			entry.DurationMillis = &ms
		}
		if g.UsageJSON != "" && g.UsageJSON != "{}" {
			entry.Usage = json.RawMessage(g.UsageJSON)
		}
		if g.TimingsJSON != "" && g.TimingsJSON != "{}" {
			entry.Timings = json.RawMessage(g.TimingsJSON)
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}
