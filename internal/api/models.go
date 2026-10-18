package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers/openai"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Models holds the dependencies for the connection test and model
// discovery handlers (ARCHITECTURE.md section 4.1).
type Models struct {
	Store     *store.Store
	Protector secrets.Protector
	Egress    *egress.Client
}

// adapterFor loads an endpoint and its credential (if any) and builds
// the one adapter every endpoint uses (PROVIDER-SPEC.md section 3).
func (m *Models) adapterFor(ctx context.Context, id int64) (*openai.Adapter, error) {
	ep, err := m.Store.GetEndpoint(ctx, id)
	if err != nil {
		return nil, err
	}
	target, err := m.targetFor(ctx, ep)
	if err != nil {
		return nil, err
	}
	return openai.New(m.Egress, target), nil
}

func (m *Models) targetFor(ctx context.Context, ep *store.Endpoint) (egress.Target, error) {
	u, err := url.Parse(ep.BaseURL)
	if err != nil {
		return egress.Target{}, fmt.Errorf("parse stored base URL: %w", err)
	}
	target := egress.Target{BaseURL: u, PrivacyClass: ep.PrivacyClass}

	hasCred, err := m.Store.HasCredential(ctx, ep.ID)
	if err != nil {
		return egress.Target{}, err
	}
	if hasCred {
		blob, err := m.Store.CredentialBlob(ctx, ep.ID)
		if err != nil {
			return egress.Target{}, err
		}
		value, err := m.Protector.Unprotect(blob)
		if err != nil {
			return egress.Target{}, fmt.Errorf("unprotect credential: %w", err)
		}
		target.Credential = value
	}
	return target, nil
}

type testResponse struct {
	DetectedService string `json:"detected_service"`
	Healthy         bool   `json:"healthy"`
}

// Test handles POST /api/endpoints/{id}/test: runs the read-only
// detection checks in PROVIDER-SPEC.md section 3 (never a control-plane
// call, SECURITY-MODEL.md section 5) and records the result as the
// endpoint's detected_service.
func (m *Models) Test(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	adapter, err := m.adapterFor(r.Context(), id)
	if err != nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}
	detection, err := adapter.Detect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := m.Store.SetDetectedService(r.Context(), id, detection.DetectedService); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, testResponse{DetectedService: detection.DetectedService, Healthy: detection.Healthy})
}

// modelResponse mirrors the capability profile in PROVIDER-SPEC.md
// section 2.
type modelResponse struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	Chat           bool      `json:"chat"`
	Streaming      bool      `json:"streaming"`
	Embeddings     bool      `json:"embeddings"`
	Vision         bool      `json:"vision"`
	Tools          bool      `json:"tools"`
	ReasoningField string    `json:"reasoning_field"`
	ContextLength  int       `json:"context_length"`
	State          string    `json:"state"`
	Concurrency    int       `json:"concurrency"`
	SeenAt         time.Time `json:"seen_at"`
}

// capabilitiesJSON is what store.Model.CapabilitiesJSON actually holds:
// everything about a model's capability profile that isn't one of the
// store's own typed columns (type, state, context_length).
type capabilitiesJSON struct {
	Chat           bool   `json:"chat"`
	Streaming      bool   `json:"streaming"`
	Embeddings     bool   `json:"embeddings"`
	Vision         bool   `json:"vision"`
	Tools          bool   `json:"tools"`
	ReasoningField string `json:"reasoning_field"`
	Concurrency    int    `json:"concurrency"`
}

// Refresh handles POST /api/endpoints/{id}/models/refresh: discovers
// models from the provider and records them in the cached inventory.
func (m *Models) Refresh(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	adapter, err := m.adapterFor(r.Context(), id)
	if err != nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}
	models, err := adapter.ListModels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	now := time.Now().UTC()
	out := make([]modelResponse, 0, len(models))
	for _, mo := range models {
		capsJSON, err := json.Marshal(capabilitiesJSON{
			Chat: mo.Chat, Streaming: mo.Streaming, Embeddings: mo.Embeddings,
			Vision: mo.Vision, Tools: mo.Tools, ReasoningField: mo.ReasoningField,
			Concurrency: mo.Concurrency,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := m.Store.UpsertModel(r.Context(), store.Model{
			EndpointID:       id,
			ModelID:          mo.ID,
			Type:             mo.Type,
			State:            mo.State,
			ContextLength:    mo.ContextLength,
			CapabilitiesJSON: string(capsJSON),
			SeenAt:           now,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, modelResponse{
			ID: mo.ID, Type: mo.Type, Chat: mo.Chat, Streaming: mo.Streaming,
			Embeddings: mo.Embeddings, Vision: mo.Vision, Tools: mo.Tools,
			ReasoningField: mo.ReasoningField, ContextLength: mo.ContextLength,
			State: mo.State, Concurrency: mo.Concurrency, SeenAt: now,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// List handles GET /api/endpoints/{id}/models: the cached inventory
// from the last refresh, with no outbound network call.
func (m *Models) List(w http.ResponseWriter, r *http.Request) {
	id, err := parseEndpointID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	models, err := m.Store.ListModels(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]modelResponse, 0, len(models))
	for _, mo := range models {
		var caps capabilitiesJSON
		_ = json.Unmarshal([]byte(mo.CapabilitiesJSON), &caps)
		out = append(out, modelResponse{
			ID: mo.ModelID, Type: mo.Type, Chat: caps.Chat, Streaming: caps.Streaming,
			Embeddings: caps.Embeddings, Vision: caps.Vision, Tools: caps.Tools,
			ReasoningField: caps.ReasoningField, ContextLength: mo.ContextLength,
			State: mo.State, Concurrency: caps.Concurrency, SeenAt: mo.SeenAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
