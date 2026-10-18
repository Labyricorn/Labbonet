package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/mods"
	"github.com/Labyricorn/Labbonet/internal/prompt"
	"github.com/Labyricorn/Labbonet/internal/search"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Mods serves MODS (macros and appearance presets) and web-search settings. The search key
// is accepted once, protected, and never returned: responses only say whether one is set.
type Mods struct {
	Store     *store.Store
	Protector secrets.Protector
	// Search is the fixed search client used by the connection test; nil disables the test.
	Search *egress.SearchClient
}

func modsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "This changed in another tab or window; reload before saving", http.StatusConflict)
	case errors.Is(err, store.ErrInvalidMods), mods.IsInvalid(err), search.IsSettingError(err):
		msg := strings.TrimPrefix(err.Error(), store.ErrInvalidMods.Error()+": ")
		http.Error(w, strings.TrimPrefix(msg, "invalid MODS data: "), http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusInternalServerError)
	}
}

// ---- macros ---------------------------------------------------------------------------

type macrosResponse struct {
	Macros   []mods.Macro `json:"macros"`
	Reserved []string     `json:"reserved"`
	Limits   macroLimits  `json:"limits"`
}

type macroLimits struct {
	Count   int `json:"count"`
	Trigger int `json:"trigger_length"`
	Bytes   int `json:"text_bytes"`
	Total   int `json:"total_bytes"`
}

// ListMacros handles GET /api/macros.
func (m *Mods) ListMacros(w http.ResponseWriter, r *http.Request) {
	list, err := m.Store.ListMacros(r.Context())
	if err != nil {
		modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, macrosResponse{Macros: list, Reserved: mods.Reserved,
		Limits: macroLimits{mods.MaxMacros, mods.MaxTriggerLen, mods.MaxMacroBytes, mods.MaxMacroTotal}})
}

// SaveMacro handles POST /api/macros (create) and PUT /api/macros/{id} (edit; the body
// carries the revision it edited).
func (m *Mods) SaveMacro(w http.ResponseWriter, r *http.Request) {
	var in mods.Macro
	if !decodeBody(w, r, &in) {
		return
	}
	in.ID = 0
	if r.PathValue("id") != "" {
		id, err := pathInt(r, "id")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		in.ID = id
		if in.Revision <= 0 {
			http.Error(w, "revision is required", http.StatusBadRequest)
			return
		}
	}
	saved, err := m.Store.SaveMacro(r.Context(), in)
	if err != nil {
		modsError(w, err)
		return
	}
	status := http.StatusOK
	if in.ID == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

// DeleteMacro handles DELETE /api/macros/{id}?revision=N.
func (m *Mods) DeleteMacro(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var rev int64
	if raw := r.URL.Query().Get("revision"); raw != "" {
		rev, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || rev < 0 {
			http.Error(w, "invalid revision", http.StatusBadRequest)
			return
		}
	}
	if err := m.Store.DeleteMacro(r.Context(), id, rev); err != nil {
		modsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PreviewMacros handles POST /api/macros/preview: expands sample text with the saved macros
// and sample built-ins. It stores and sends nothing.
func (m *Mods) PreviewMacros(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	if len(in.Text) > mods.PreviewMaxSize {
		http.Error(w, "preview text is too long", http.StatusBadRequest)
		return
	}
	list, err := m.Store.ListMacros(r.Context())
	if err != nil {
		modsError(w, err)
		return
	}
	values := mods.Values(list)
	now := time.Now()
	for k, v := range map[string]string{"char": "Character", "user": "You", "date": now.Format("2006-01-02"), "time": now.Format("15:04"), "carousel": ""} {
		values[k] = v
	}
	out, err := prompt.Expand(in.Text, values)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": out})
}

// ---- appearance -----------------------------------------------------------------------

type appearanceResponse struct {
	mods.Appearance
	Builtin   []mods.Preset     `json:"builtin"`
	Effective map[string]string `json:"effective"`
	Names     []string          `json:"adjustable"`
}

func appearanceOut(a mods.Appearance) appearanceResponse {
	return appearanceResponse{Appearance: a, Builtin: mods.Builtin(), Effective: a.Effective(), Names: mods.VarNames()}
}

// GetAppearance handles GET /api/appearance.
func (m *Mods) GetAppearance(w http.ResponseWriter, r *http.Request) {
	a, err := m.Store.GetAppearance(r.Context())
	if err != nil {
		modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, appearanceOut(a))
}

// PutAppearance handles PUT /api/appearance; the body carries the revision it edited.
func (m *Mods) PutAppearance(w http.ResponseWriter, r *http.Request) {
	var in mods.Appearance
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Revision <= 0 {
		http.Error(w, "revision is required", http.StatusBadRequest)
		return
	}
	a, err := m.Store.SaveAppearance(r.Context(), in)
	if err != nil {
		modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, appearanceOut(a))
}

// ---- web search -----------------------------------------------------------------------

type searchSettingsResponse struct {
	search.Settings
	CredentialSet bool   `json:"credential_set"`
	Service       string `json:"service"`
}

func (m *Mods) searchState(r *http.Request) (searchSettingsResponse, error) {
	s, err := m.Store.GetSearchSettings(r.Context())
	if err != nil {
		return searchSettingsResponse{}, err
	}
	blob, err := m.Store.SearchCredentialBlob(r.Context())
	if err != nil {
		return searchSettingsResponse{}, err
	}
	return searchSettingsResponse{Settings: s, CredentialSet: len(blob) > 0, Service: egress.SearchHost}, nil
}

// GetSearchSettings handles GET /api/search/settings.
func (m *Mods) GetSearchSettings(w http.ResponseWriter, r *http.Request) {
	out, err := m.searchState(r)
	if err != nil {
		modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// PutSearchSettings handles PUT /api/search/settings.
func (m *Mods) PutSearchSettings(w http.ResponseWriter, r *http.Request) {
	var in search.Settings
	if !decodeBody(w, r, &in) {
		return
	}
	if in.Revision <= 0 {
		http.Error(w, "revision is required", http.StatusBadRequest)
		return
	}
	if _, err := m.Store.SaveSearchSettings(r.Context(), in); err != nil {
		modsError(w, err)
		return
	}
	m.GetSearchSettings(w, r)
}

// PutSearchKey handles PUT /api/search/credential: the key is protected and only the
// opaque blob is stored. It is never echoed or logged.
func (m *Mods) PutSearchKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Value string `json:"value"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	key := strings.TrimSpace(in.Value)
	if key == "" || len(key) > 512 || strings.ContainsAny(key, "\x00\r\n\t ") {
		http.Error(w, "the key must be a single token of up to 512 characters", http.StatusBadRequest)
		return
	}
	blob, err := m.Protector.Protect(key)
	if err != nil {
		http.Error(w, "credential storage is unavailable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := m.Store.SetSearchCredential(r.Context(), blob); err != nil {
		modsError(w, err)
		return
	}
	m.GetSearchSettings(w, r)
}

// DeleteSearchKey handles DELETE /api/search/credential.
func (m *Mods) DeleteSearchKey(w http.ResponseWriter, r *http.Request) {
	if err := m.Store.ClearSearchCredential(r.Context()); err != nil {
		modsError(w, err)
		return
	}
	m.GetSearchSettings(w, r)
}

// TestSearch handles POST /api/search/test: the one explicit request that sends a fixed
// test phrase (never conversation text) to the search service with the stored key. It runs
// even while search is switched off, because it is the owner's own action.
func (m *Mods) TestSearch(w http.ResponseWriter, r *http.Request) {
	if m.Search == nil {
		http.Error(w, "web search is not available in this build", http.StatusServiceUnavailable)
		return
	}
	blob, err := m.Store.SearchCredentialBlob(r.Context())
	if err != nil {
		modsError(w, err)
		return
	}
	if len(blob) == 0 {
		http.Error(w, search.ErrNoKey.Error(), http.StatusBadRequest)
		return
	}
	key, err := m.Protector.Unprotect(blob)
	if err != nil {
		http.Error(w, "the stored key could not be unprotected; store it again", http.StatusInternalServerError)
		return
	}
	hits, err := search.Run(r.Context(), m.Search, key, search.ConnectionTestQuery, 1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": len(hits)})
}

// SearchRecords handles GET /api/conversations/{id}/search: what was searched for and found
// for the conversation's recent replies, newest first.
func (m *Mods) SearchRecords(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := m.Store.GetConversation(r.Context(), id); err != nil {
		modsError(w, err)
		return
	}
	records, err := m.Store.ListSearchRecords(r.Context(), id, 50)
	if err != nil {
		modsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}
