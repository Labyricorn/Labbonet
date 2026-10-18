// Package api holds the JSON handlers served under /api.
package api

import (
	"encoding/json"
	"net/http"
)

// Version is the Labbonet build version. Phase 1 has no release process
// yet, so this is a fixed placeholder.
const Version = "0.1.0-dev"

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Health handles GET /api/health.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: Version})
}
