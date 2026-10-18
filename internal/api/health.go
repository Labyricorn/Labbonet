// Package api holds the JSON handlers served under /api.
package api

import (
	"encoding/json"
	"net/http"
)

// Version is the current owner-designated development version, not a release tag.
const Version = "V0-2-0"

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Health handles GET /api/health.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: Version})
}
