package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/profile"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Profiles serves settings profiles: named bundles of the theme, memory settings and chat
// limits. A profile is captured from the current settings on the server (the browser only
// supplies its own limits) and applying one copies the values back; profiles never touch
// threads, characters, endpoints or credentials.
type Profiles struct {
	Store *store.Store
}

func profileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "This changed in another tab or window; reload before saving", http.StatusConflict)
	case errors.Is(err, store.ErrInvalidProfile), profile.IsInvalid(err):
		msg := strings.TrimPrefix(err.Error(), store.ErrInvalidProfile.Error()+": ")
		http.Error(w, strings.TrimPrefix(msg, "invalid profile: "), http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusInternalServerError)
	}
}

type profilesResponse struct {
	Profiles []profile.Profile `json:"profiles"`
	ActiveID int64             `json:"active_id"`
	Limits   struct {
		Count int `json:"count"`
		Name  int `json:"name_runes"`
	} `json:"limits"`
}

// List handles GET /api/profiles.
func (h *Profiles) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListProfiles(r.Context())
	if err != nil {
		profileError(w, err)
		return
	}
	active, err := h.Store.ActiveProfile(r.Context())
	if err != nil {
		profileError(w, err)
		return
	}
	resp := profilesResponse{Profiles: list, ActiveID: active}
	resp.Limits.Count, resp.Limits.Name = profile.MaxProfiles, profile.MaxNameRunes
	writeJSON(w, http.StatusOK, resp)
}

type profileRequest struct {
	Name     string        `json:"name"`
	Revision int64         `json:"revision"`
	Prefs    profile.Prefs `json:"prefs"`
	// Memory says whether the memory settings are part of the profile.
	Memory bool `json:"memory"`
	// Capture, on an update, re-reads the current settings into the profile.
	Capture bool `json:"capture"`
}

func (h *Profiles) capture(r *http.Request, in profileRequest) (profile.Data, error) {
	ap, err := h.Store.GetAppearance(r.Context())
	if err != nil {
		return profile.Data{}, err
	}
	d := profile.Data{Appearance: ap.Active, Prefs: in.Prefs}
	if in.Memory {
		mem, err := h.Store.GetMemorySettings(r.Context())
		if err != nil {
			return profile.Data{}, err
		}
		d.Memory = &mem
	}
	return d, nil
}

// Save handles POST /api/profiles (capture the current settings under a name) and PUT
// /api/profiles/{id} (rename, or with capture re-read the current settings).
func (h *Profiles) Save(w http.ResponseWriter, r *http.Request) {
	var in profileRequest
	if !decodeBody(w, r, &in) {
		return
	}
	p := profile.Profile{Name: in.Name}
	if r.PathValue("id") != "" {
		id, err := pathInt(r, "id")
		if err != nil || in.Revision <= 0 {
			http.Error(w, "a valid id and revision are required", http.StatusBadRequest)
			return
		}
		cur, err := h.Store.GetProfile(r.Context(), id)
		if err != nil {
			profileError(w, err)
			return
		}
		p.ID, p.Revision, p.Data = id, in.Revision, cur.Data
		if in.Name == "" {
			p.Name = cur.Name
		}
	}
	if p.ID == 0 || in.Capture {
		d, err := h.capture(r, in)
		if err != nil {
			profileError(w, err)
			return
		}
		p.Data = d
	}
	saved, err := h.Store.SaveProfile(r.Context(), p, time.Now())
	if err != nil {
		profileError(w, err)
		return
	}
	status := http.StatusOK
	if p.ID == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

// Delete handles DELETE /api/profiles/{id}?revision=N.
func (h *Profiles) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var rev int64
	if raw := r.URL.Query().Get("revision"); raw != "" {
		if rev, err = strconv.ParseInt(raw, 10, 64); err != nil || rev < 0 {
			http.Error(w, "invalid revision", http.StatusBadRequest)
			return
		}
	}
	if err := h.Store.DeleteProfile(r.Context(), id, rev); err != nil {
		profileError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type applyResponse struct {
	Applied []string      `json:"applied"`
	Notes   []string      `json:"notes"`
	Prefs   profile.Prefs `json:"prefs"`
}

// Apply handles POST /api/profiles/{id}/apply. Each part is applied independently; a part
// that can no longer be applied (a deleted theme, a model that left the inventory) is
// reported in notes and does not stop the others.
func (h *Profiles) Apply(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := h.Store.GetProfile(r.Context(), id)
	if err != nil {
		profileError(w, err)
		return
	}
	resp := applyResponse{Applied: []string{}, Notes: []string{}, Prefs: p.Data.Prefs}

	ap, err := h.Store.GetAppearance(r.Context())
	if err != nil {
		profileError(w, err)
		return
	}
	ap.Active = p.Data.Appearance
	if _, err := h.Store.SaveAppearance(r.Context(), ap); err != nil {
		if !errors.Is(err, store.ErrInvalidMods) {
			profileError(w, err)
			return
		}
		resp.Notes = append(resp.Notes, "Theme not applied: that preset no longer exists.")
	} else {
		resp.Applied = append(resp.Applied, "theme")
	}

	if p.Data.Memory != nil {
		cur, err := h.Store.GetMemorySettings(r.Context())
		if err != nil {
			profileError(w, err)
			return
		}
		want := *p.Data.Memory
		want.Revision = cur.Revision
		if _, err := h.Store.SaveMemorySettings(r.Context(), want); err != nil {
			if !errors.Is(err, store.ErrInvalidMemory) {
				profileError(w, err)
				return
			}
			resp.Notes = append(resp.Notes, "Memory settings not applied: "+strings.TrimPrefix(err.Error(), store.ErrInvalidMemory.Error()+": "))
		} else {
			resp.Applied = append(resp.Applied, "memory")
		}
	}
	resp.Applied = append(resp.Applied, "chat limits")
	if err := h.Store.SetActiveProfile(r.Context(), id); err != nil {
		profileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
