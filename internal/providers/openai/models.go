package openai

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

// Strata /v1/models shape (Strata_Protocol.md section 5.2). Only the
// loaded model is listed.
type strataModelList struct {
	Data []strataModelEntry `json:"data"`
}

type strataModelEntry struct {
	ID     string `json:"id"`
	Status struct {
		Value string `json:"value"`
	} `json:"status"`
	Meta struct {
		NCtx int `json:"n_ctx"`
	} `json:"meta"`
	Architecture struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
}

// LM Studio /api/v0/models shape (PROVIDER-SPEC.md section 5.1).
type lmStudioModelList struct {
	Data []lmStudioModelEntry `json:"data"`
}

type lmStudioModelEntry struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"` // "llm", "vlm" or "embeddings"
	State            string   `json:"state"`
	MaxContextLength int      `json:"max_context_length"`
	Capabilities     []string `json:"capabilities"`
}

// Generic OpenAI-compatible /v1/models shape: just IDs.
type genericModelList struct {
	Data []genericModelEntry `json:"data"`
}

type genericModelEntry struct {
	ID string `json:"id"`
}

// ListModels discovers models using the shape matching this Adapter's
// detected profile (PROVIDER-SPEC.md section 2), running Detect first
// if it has not run yet.
func (a *Adapter) ListModels(ctx context.Context) ([]providers.Model, error) {
	if a.profile == "" {
		if _, err := a.Detect(ctx); err != nil {
			return nil, err
		}
	}
	switch a.profile {
	case ProfileStrata:
		return a.listStrataModels(ctx)
	case ProfileLMStudio:
		return a.listLMStudioModels(ctx)
	default:
		return a.listGenericModels(ctx)
	}
}

func (a *Adapter) listStrataModels(ctx context.Context) ([]providers.Model, error) {
	resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, mapRequestError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, mapHTTPError(resp.StatusCode, data)
	}
	var list strataModelList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, &providers.Error{Kind: providers.ErrProtocolError, Message: "malformed model list", Err: err}
	}

	out := make([]providers.Model, 0, len(list.Data))
	for _, m := range list.Data {
		vision := false
		for _, modality := range m.Architecture.InputModalities {
			if modality == "image" {
				vision = true
			}
		}
		state := m.Status.Value
		if state == "" {
			// Strata only ever lists the one model it has loaded.
			state = "loaded"
		}
		out = append(out, providers.Model{
			ID:             m.ID,
			Type:           "llm",
			Chat:           true,
			Streaming:      true,
			Vision:         vision,
			ReasoningField: "reasoning_content",
			ContextLength:  m.Meta.NCtx,
			State:          state,
			Concurrency:    1,
		})
	}
	return out, nil
}

func (a *Adapter) listLMStudioModels(ctx context.Context) ([]providers.Model, error) {
	resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/api/v0/models", nil)
	if err != nil {
		return nil, mapRequestError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, mapHTTPError(resp.StatusCode, data)
	}
	var list lmStudioModelList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, &providers.Error{Kind: providers.ErrProtocolError, Message: "malformed model list", Err: err}
	}

	out := make([]providers.Model, 0, len(list.Data))
	for _, m := range list.Data {
		typ := m.Type
		if typ == "" {
			typ = "llm"
		}
		tools := false
		for _, c := range m.Capabilities {
			if c == "tool_use" {
				tools = true
			}
		}
		out = append(out, providers.Model{
			ID:            m.ID,
			Type:          typ,
			Chat:          typ != "embeddings",
			Streaming:     typ != "embeddings",
			Embeddings:    typ == "embeddings",
			Vision:        typ == "vlm",
			Tools:         tools,
			ContextLength: m.MaxContextLength,
			State:         m.State,
			Concurrency:   1,
		})
	}
	return out, nil
}

func (a *Adapter) listGenericModels(ctx context.Context) ([]providers.Model, error) {
	resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, mapRequestError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, mapHTTPError(resp.StatusCode, data)
	}
	var list genericModelList
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, &providers.Error{Kind: providers.ErrProtocolError, Message: "malformed model list", Err: err}
	}

	out := make([]providers.Model, 0, len(list.Data))
	for _, m := range list.Data {
		out = append(out, providers.Model{
			ID:          m.ID,
			Type:        "llm",
			Chat:        true,
			Streaming:   true,
			Concurrency: 1,
			// State is left empty: a generic OpenAI-compatible server's
			// /v1/models does not report load state (only Strata and LM
			// Studio do, each in their own listStrata/listLMStudio path).
		})
	}
	return out, nil
}
