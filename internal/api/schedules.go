package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/schedule"
	"github.com/Labyricorn/Labbonet/internal/store"
)

// Schedules serves SCHED: scheduled prompts, their run ledger and the scheduler's status.
type Schedules struct {
	Store *store.Store
	Sched *generate.Scheduler
}

func scheduleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "This changed in another tab or window; reload before saving", http.StatusConflict)
	case errors.Is(err, generate.ErrScheduleBusy):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, generate.ErrScheduleNoRun):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, store.ErrInvalidSchedule), schedule.IsInvalid(err):
		msg := strings.TrimPrefix(err.Error(), store.ErrInvalidSchedule.Error()+": ")
		http.Error(w, strings.TrimPrefix(msg, "invalid schedule: "), http.StatusBadRequest)
	default:
		http.Error(w, "request failed", http.StatusInternalServerError)
	}
}

type scheduleLimits struct {
	Count         int    `json:"count"`
	NameRunes     int    `json:"name_runes"`
	PromptBytes   int    `json:"prompt_bytes"`
	AutoContinue  int    `json:"auto_continue"`
	ContinueText  string `json:"continue_prompt"`
	ServerZone    string `json:"server_timezone"`
	UnattendedCap int    `json:"unattended_reasoning_limit"`
}

type schedulesResponse struct {
	Schedules []schedule.Schedule      `json:"schedules"`
	Status    generate.SchedulerStatus `json:"status"`
	Limits    scheduleLimits           `json:"limits"`
}

// List handles GET /api/schedules.
func (h *Schedules) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListSchedules(r.Context())
	if err != nil {
		scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, schedulesResponse{Schedules: list, Status: h.Sched.Status(), Limits: scheduleLimits{
		Count: schedule.MaxSchedules, NameRunes: schedule.MaxNameRunes, PromptBytes: schedule.MaxPromptBytes,
		AutoContinue: schedule.MaxAutoContinue, ContinueText: schedule.ContinuePrompt, ServerZone: time.Local.String(),
		UnattendedCap: generate.UnattendedReasoningLimit,
	}})
}

// Save handles POST /api/schedules (create) and PUT /api/schedules/{id} (edit with the
// revision that was loaded).
func (h *Schedules) Save(w http.ResponseWriter, r *http.Request) {
	var in schedule.Schedule
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
	saved, err := h.Store.SaveSchedule(r.Context(), in, time.Now())
	if err != nil {
		scheduleError(w, err)
		return
	}
	status := http.StatusOK
	if in.ID == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

// Duplicate handles POST /api/schedules/{id}/duplicate.
func (h *Schedules) Duplicate(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := h.Store.DuplicateSchedule(r.Context(), id, time.Now())
	if err != nil {
		scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

// Delete handles DELETE /api/schedules/{id}?revision=N.
func (h *Schedules) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Store.DeleteSchedule(r.Context(), id, rev); err != nil {
		scheduleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Run handles POST /api/schedules/{id}/run: send the prompt now, once, without changing the
// schedule. It is the owner's own action and follows the same refusals as a timed run.
func (h *Schedules) Run(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	run, err := h.Sched.RunNow(r.Context(), id)
	if err != nil {
		scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

// Runs handles GET /api/schedules/{id}/runs.
func (h *Schedules) Runs(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := h.Store.GetSchedule(r.Context(), id); err != nil {
		scheduleError(w, err)
		return
	}
	runs, err := h.Store.ListScheduleRuns(r.Context(), id, 50)
	if err != nil {
		scheduleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// CancelRun handles POST /api/schedule-runs/{id}/cancel.
func (h *Schedules) CancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, cancelResponse{Cancelled: h.Sched.CancelRun(id)})
}
