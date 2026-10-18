package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Labyricorn/Labbonet/internal/endpoints"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Endpoints holds the dependencies for the /api/endpoints handlers
// (ARCHITECTURE.md section 4.1).
type Endpoints struct {
	Store     *store.Store
	Protector secrets.Protector
}

// endpointResponse never carries the credential itself, only whether
// one is set (SECURITY-MODEL.md S12).
type endpointResponse struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	ProviderType     string    `json:"provider_type"`
	BaseURL          string    `json:"base_url"`
	PrivacyClass     string    `json:"privacy_class"`
	Enabled          bool      `json:"enabled"`
	ConcurrencyLimit int       `json:"concurrency_limit"`
	DetectedService  string    `json:"detected_service"`
	CreatedAt        time.Time `json:"created_at"`
	CredentialSet    bool      `json:"credential_set"`
}

func (e *Endpoints) toResponse(ctx context.Context, ep *store.Endpoint) (endpointResponse, error) {
	hasCred, err := e.Store.HasCredential(ctx, ep.ID)
	if err != nil {
		return endpointResponse{}, fmt.Errorf("check credential: %w", err)
	}
	return endpointResponse{
		ID:               ep.ID,
		Name:             ep.Name,
		ProviderType:     ep.ProviderType,
		BaseURL:          ep.BaseURL,
		PrivacyClass:     ep.PrivacyClass,
		Enabled:          ep.Enabled,
		ConcurrencyLimit: ep.ConcurrencyLimit,
		DetectedService:  ep.DetectedService,
		CreatedAt:        ep.CreatedAt,
		CredentialSet:    hasCred,
	}, nil
}

// List handles GET /api/endpoints.
func (e *Endpoints) List(w http.ResponseWriter, r *http.Request) {
	eps, err := e.Store.ListEndpoints(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]endpointResponse, 0, len(eps))
	for _, ep := range eps {
		resp, err := e.toResponse(r.Context(), ep)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, out)
}

type createEndpointRequest struct {
	Name                string `json:"name"`
	BaseURL             string `json:"base_url"`
	ConcurrencyLimit    int    `json:"concurrency_limit"`
	AcknowledgeExternal bool   `json:"acknowledge_external"`
}

// Create handles POST /api/endpoints: validates the base URL,
// classifies its privacy class (refusing metadata addresses and
// requiring acknowledgement for external ones), and registers it
// (SECURITY-MODEL.md section 5). Every endpoint uses the one "openai"
// adapter (PROVIDER-SPEC.md section 3); provider detection happens in
// the connection test, not here.
func (e *Endpoints) Create(w http.ResponseWriter, r *http.Request) {
	var req createEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	u, err := endpoints.ValidateBaseURL(req.BaseURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	concurrency := req.ConcurrencyLimit
	if concurrency <= 0 {
		concurrency = 1
	}

	class, err := endpoints.ClassifyAddress(r.Context(), u.Hostname(), req.AcknowledgeExternal)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, endpoints.ErrExternalNeedsAcknowledgement) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	ep, err := e.Store.CreateEndpoint(r.Context(), req.Name, "openai", u.String(), class, concurrency)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := e.toResponse(r.Context(), ep)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

type patchEndpointRequest struct {
	Name             *string `json:"name"`
	Enabled          *bool   `json:"enabled"`
	ConcurrencyLimit *int    `json:"concurrency_limit"`
}

// Patch handles PATCH /api/endpoints/{id}: edits name, enabled and
// concurrency_limit. The base URL and privacy class are fixed at
// registration; delete and re-register to change the host.
func (e *Endpoints) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := e.Store.GetEndpoint(r.Context(), id)
	if err != nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	var req patchEndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	name := existing.Name
	if req.Name != nil {
		if *req.Name == "" {
			http.Error(w, "name cannot be empty", http.StatusBadRequest)
			return
		}
		name = *req.Name
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	concurrency := existing.ConcurrencyLimit
	if req.ConcurrencyLimit != nil {
		if *req.ConcurrencyLimit <= 0 {
			http.Error(w, "concurrency_limit must be positive", http.StatusBadRequest)
			return
		}
		concurrency = *req.ConcurrencyLimit
	}

	ep, err := e.Store.UpdateEndpoint(r.Context(), id, name, enabled, concurrency)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := e.toResponse(r.Context(), ep)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// Delete handles DELETE /api/endpoints/{id}; its credential and model
// inventory go with it via ON DELETE CASCADE.
func (e *Endpoints) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := e.Store.DeleteEndpoint(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setCredentialRequest struct {
	Value string `json:"value"`
}

// SetCredential handles PUT /api/endpoints/{id}/credential: protects
// the value (DPAPI) and stores only the opaque blob. The value is never
// echoed back or logged (SECURITY-MODEL.md S12, S13).
func (e *Endpoints) SetCredential(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := e.Store.GetEndpoint(r.Context(), id); err != nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	var req setCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		http.Error(w, "value is required", http.StatusBadRequest)
		return
	}

	blob, err := e.Protector.Protect(req.Value)
	if err != nil {
		http.Error(w, "credential storage is unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := e.Store.SetCredential(r.Context(), id, blob); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ClearCredential handles DELETE /api/endpoints/{id}/credential.
func (e *Endpoints) ClearCredential(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := e.Store.ClearCredential(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseEndpointID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid endpoint id %q", raw)
	}
	return id, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
