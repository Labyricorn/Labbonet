// Package openai is Labbonet's one provider adapter (PROVIDER-SPEC.md
// section 1): every registered endpoint, Strata or LM Studio or
// otherwise, talks through this adapter. A detected "profile" only
// selects the model-list shape and the chat extras allowlist; the
// request paths themselves are the fixed set in internal/egress.
package openai

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
)

// Detected service hints (PROVIDER-SPEC.md section 3).
const (
	ProfileStrata   = "strata"
	ProfileLMStudio = "lm-studio"
	ProfileGeneric  = "openai-compatible"
)

// Adapter implements providers.Provider for one endpoint.
type Adapter struct {
	egress  *egress.Client
	target  egress.Target
	profile string
}

// New builds an Adapter for one endpoint. client is normally shared
// across endpoints; target carries this endpoint's base URL, privacy
// class and credential (internal/egress.Target), so a credential never
// travels with any other endpoint's Adapter.
func New(client *egress.Client, target egress.Target) *Adapter {
	return &Adapter{egress: client, target: target}
}

// Profile reports the detected service hint ("" if Detect has not run
// yet).
func (a *Adapter) Profile() string {
	return a.profile
}

type strataHealth struct {
	Service string `json:"service"`
}

type lmStudioProbeEntry struct {
	State string `json:"state"`
	Type  string `json:"type"`
}

type lmStudioProbe struct {
	Data []lmStudioProbeEntry `json:"data"`
}

// Detect runs the read-only checks in PROVIDER-SPEC.md section 3 and
// records the result as this Adapter's profile for ListModels and
// StreamChat. It never calls a control-plane endpoint (SECURITY-
// MODEL.md section 5).
func (a *Adapter) Detect(ctx context.Context) (providers.Detection, error) {
	if resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/health", nil); err == nil && resp.StatusCode == http.StatusOK {
		var h strataHealth
		if json.Unmarshal(data, &h) == nil && h.Service == "strata" {
			a.profile = ProfileStrata
			return providers.Detection{DetectedService: ProfileStrata, Healthy: true}, nil
		}
	}

	if resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/api/v0/models", nil); err == nil && resp.StatusCode == http.StatusOK {
		var probe lmStudioProbe
		if json.Unmarshal(data, &probe) == nil && len(probe.Data) > 0 && probe.Data[0].State != "" && probe.Data[0].Type != "" {
			a.profile = ProfileLMStudio
			return providers.Detection{DetectedService: ProfileLMStudio, Healthy: true}, nil
		}
	}

	if resp, _, err := a.egress.DoLimited(ctx, a.target, http.MethodGet, "/v1/models", nil); err == nil && resp.StatusCode == http.StatusOK {
		a.profile = ProfileGeneric
		return providers.Detection{DetectedService: ProfileGeneric, Healthy: true}, nil
	}

	return providers.Detection{}, &providers.Error{
		Kind:    providers.ErrUnreachable,
		Message: "no OpenAI-compatible endpoint responded to /health, /api/v0/models or /v1/models",
	}
}
