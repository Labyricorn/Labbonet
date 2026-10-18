package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/generate"
	"github.com/Labyricorn/Labbonet/internal/mods"
	"github.com/Labyricorn/Labbonet/internal/profile"
	"github.com/Labyricorn/Labbonet/internal/schedule"
	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newSchedMux(t *testing.T) (*http.ServeMux, *store.Store, int64) {
	t.Helper()
	st := storetest.New(t)
	conv, err := st.CreateConversation(context.Background(), "T", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	rn := &generate.Runner{Store: st}
	sched := &generate.Scheduler{Store: st, Jobs: generate.NewJobs(rn)}
	h := &Schedules{Store: st, Sched: sched}
	p := &Profiles{Store: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schedules", h.List)
	mux.HandleFunc("POST /api/schedules", h.Save)
	mux.HandleFunc("PUT /api/schedules/{id}", h.Save)
	mux.HandleFunc("DELETE /api/schedules/{id}", h.Delete)
	mux.HandleFunc("POST /api/schedules/{id}/duplicate", h.Duplicate)
	mux.HandleFunc("POST /api/schedules/{id}/run", h.Run)
	mux.HandleFunc("GET /api/schedules/{id}/runs", h.Runs)
	mux.HandleFunc("POST /api/schedule-runs/{id}/cancel", h.CancelRun)
	mux.HandleFunc("GET /api/profiles", p.List)
	mux.HandleFunc("POST /api/profiles", p.Save)
	mux.HandleFunc("PUT /api/profiles/{id}", p.Save)
	mux.HandleFunc("DELETE /api/profiles/{id}", p.Delete)
	mux.HandleFunc("POST /api/profiles/{id}/apply", p.Apply)
	mux.HandleFunc("GET /api/appearance", (&Mods{Store: st}).GetAppearance)
	return mux, st, conv.ID
}

func tomorrow() string { return time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02") }

func TestScheduleAPILifecycleAndConflicts(t *testing.T) {
	mux, _, cid := newSchedMux(t)
	body := map[string]any{"name": "Morning", "conversation_id": cid, "prompt": "Hello", "kind": "daily", "timezone": "UTC", "time": "07:30", "enabled": true, "auto_continue": 1}
	rr := doJSON(t, mux, "POST", "/api/schedules", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body)
	}
	s := decode[schedule.Schedule](t, rr)
	if s.NextRun == nil || !s.NextRun.After(time.Now()) || s.Revision != 1 || !s.Enabled {
		t.Fatalf("created = %+v", s)
	}

	body["revision"], body["name"] = s.Revision, "Evening"
	rr = doJSON(t, mux, "PUT", "/api/schedules/"+strconv.FormatInt(s.ID, 10), body)
	if rr.Code != http.StatusOK || decode[schedule.Schedule](t, rr).Revision != 2 {
		t.Fatalf("edit = %d %s", rr.Code, rr.Body)
	}
	// Saving again with the old revision is a conflict, not a silent overwrite.
	if rr = doJSON(t, mux, "PUT", "/api/schedules/"+strconv.FormatInt(s.ID, 10), body); rr.Code != http.StatusConflict {
		t.Fatalf("stale edit = %d", rr.Code)
	}
	delete(body, "revision")
	if rr = doJSON(t, mux, "PUT", "/api/schedules/"+strconv.FormatInt(s.ID, 10), body); rr.Code != http.StatusBadRequest {
		t.Fatalf("edit without revision = %d", rr.Code)
	}

	rr = doJSON(t, mux, "POST", "/api/schedules/"+strconv.FormatInt(s.ID, 10)+"/duplicate", nil)
	dup := decode[schedule.Schedule](t, rr)
	if rr.Code != http.StatusCreated || dup.Enabled || dup.Name != "Evening copy" || dup.NextRun != nil {
		t.Fatalf("duplicate = %d %+v", rr.Code, dup)
	}

	list := decode[schedulesResponse](t, doJSON(t, mux, "GET", "/api/schedules", nil))
	if len(list.Schedules) != 2 || list.Limits.AutoContinue != schedule.MaxAutoContinue || list.Limits.ContinueText != schedule.ContinuePrompt {
		t.Fatalf("list = %+v", list)
	}

	if rr = doJSON(t, mux, "DELETE", "/api/schedules/"+strconv.FormatInt(dup.ID, 10)+"?revision=99", nil); rr.Code != http.StatusConflict {
		t.Fatalf("stale delete = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "DELETE", "/api/schedules/"+strconv.FormatInt(dup.ID, 10)+"?revision=1", nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "DELETE", "/api/schedules/"+strconv.FormatInt(dup.ID, 10), nil); rr.Code != http.StatusNotFound {
		t.Fatalf("delete again = %d", rr.Code)
	}
}

func TestScheduleAPIRejectsInvalidSchedules(t *testing.T) {
	mux, _, cid := newSchedMux(t)
	good := func() map[string]any {
		return map[string]any{"name": "N", "conversation_id": cid, "prompt": "p", "kind": "daily", "timezone": "UTC", "time": "07:30"}
	}
	cases := map[string]func(map[string]any){
		"bad zone":        func(m map[string]any) { m["timezone"] = "Nowhere/Land" },
		"bad time":        func(m map[string]any) { m["time"] = "7pm" },
		"missing thread":  func(m map[string]any) { m["conversation_id"] = 9999 },
		"no thread":       func(m map[string]any) { delete(m, "conversation_id") },
		"past one-off":    func(m map[string]any) { m["kind"], m["date"], m["enabled"] = "once", "2020-01-01", true },
		"too long chain":  func(m map[string]any) { m["auto_continue"] = 99 },
		"empty prompt":    func(m map[string]any) { m["prompt"] = "   " },
		"unknown kind":    func(m map[string]any) { m["kind"] = "hourly" },
		"oversize prompt": func(m map[string]any) { m["prompt"] = strings.Repeat("x", schedule.MaxPromptBytes+1) },
	}
	for name, mutate := range cases {
		m := good()
		mutate(m)
		if rr := doJSON(t, mux, "POST", "/api/schedules", m); rr.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", name, rr.Code, rr.Body)
		}
	}
	// A past one-off can be kept as a switched-off record.
	m := good()
	m["kind"], m["date"], m["enabled"] = "once", "2020-01-01", false
	if rr := doJSON(t, mux, "POST", "/api/schedules", m); rr.Code != http.StatusCreated {
		t.Fatalf("disabled past one-off = %d %s", rr.Code, rr.Body)
	}
	if rr := doJSON(t, mux, "POST", "/api/schedules/9999/run", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("run missing = %d", rr.Code)
	}
}

func TestScheduleAPIIsLimitedInCount(t *testing.T) {
	mux, st, cid := newSchedMux(t)
	for i := 0; i < schedule.MaxSchedules; i++ {
		s := schedule.Schedule{Name: "s", ConversationID: &cid, Prompt: "p", Kind: "daily", Timezone: "UTC", Time: "07:30"}
		if _, err := st.SaveSchedule(context.Background(), s, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	rr := doJSON(t, mux, "POST", "/api/schedules", map[string]any{"name": "x", "conversation_id": cid, "prompt": "p", "kind": "daily", "timezone": "UTC", "time": "07:30"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("over the limit = %d", rr.Code)
	}
}

func TestProfileCaptureApplyAndNotes(t *testing.T) {
	mux, st, _ := newSchedMux(t)
	ctx := context.Background()
	// Current theme: a custom preset.
	ap, _ := st.GetAppearance(ctx)
	ap.Presets = append(ap.Presets, presetOf("Calm", "--cyan-bright", "#112233"))
	ap, err := st.SaveAppearance(ctx, ap)
	if err != nil {
		t.Fatal(err)
	}
	ap.Active = ap.Presets[0].ID
	if ap, err = st.SaveAppearance(ctx, ap); err != nil {
		t.Fatal(err)
	}
	mem, _ := st.GetMemorySettings(ctx)
	mem.WarmthTurns = 7
	if _, err = st.SaveMemorySettings(ctx, mem); err != nil {
		t.Fatal(err)
	}

	rr := doJSON(t, mux, "POST", "/api/profiles", map[string]any{"name": "Focus", "memory": true, "prefs": map[string]any{"reply_cap": 800, "follow": true}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body)
	}
	p := decode[profile.Profile](t, rr)
	if p.Data.Appearance != ap.Active || p.Data.Memory == nil || p.Data.Memory.WarmthTurns != 7 || p.Data.Prefs.ReplyCap != 800 {
		t.Fatalf("captured = %+v", p.Data)
	}
	if rr = doJSON(t, mux, "POST", "/api/profiles", map[string]any{"name": "focus"}); rr.Code != http.StatusBadRequest {
		t.Fatalf("duplicate name (ignoring case) = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "POST", "/api/profiles", map[string]any{"name": "Bad", "prefs": map[string]any{"reply_cap": 5}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad prefs = %d", rr.Code)
	}

	// Change both settings away, then apply the profile.
	cur, _ := st.GetAppearance(ctx)
	cur.Active = ""
	if _, err = st.SaveAppearance(ctx, cur); err != nil {
		t.Fatal(err)
	}
	m2, _ := st.GetMemorySettings(ctx)
	m2.WarmthTurns = 1
	if _, err = st.SaveMemorySettings(ctx, m2); err != nil {
		t.Fatal(err)
	}
	rr = doJSON(t, mux, "POST", "/api/profiles/"+strconv.FormatInt(p.ID, 10)+"/apply", nil)
	res := decode[applyResponse](t, rr)
	if rr.Code != http.StatusOK || len(res.Notes) != 0 || res.Prefs.ReplyCap != 800 {
		t.Fatalf("apply = %d %+v", rr.Code, res)
	}
	got, _ := st.GetAppearance(ctx)
	gm, _ := st.GetMemorySettings(ctx)
	if got.Active != ap.Active || gm.WarmthTurns != 7 {
		t.Fatalf("after apply: theme %q, warmth %d", got.Active, gm.WarmthTurns)
	}
	list := decode[profilesResponse](t, doJSON(t, mux, "GET", "/api/profiles", nil))
	if list.ActiveID != p.ID {
		t.Fatalf("active = %d", list.ActiveID)
	}

	// Delete the preset the profile points at: the theme part is reported, memory still applies.
	got.Presets, got.Active = nil, ""
	if _, err = st.SaveAppearance(ctx, got); err != nil {
		t.Fatal(err)
	}
	rr = doJSON(t, mux, "POST", "/api/profiles/"+strconv.FormatInt(p.ID, 10)+"/apply", nil)
	res = decode[applyResponse](t, rr)
	if rr.Code != http.StatusOK || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "Theme") {
		t.Fatalf("apply with missing theme = %d %+v", rr.Code, res)
	}

	// Rename needs the current revision; capture re-reads settings.
	rr = doJSON(t, mux, "PUT", "/api/profiles/"+strconv.FormatInt(p.ID, 10), map[string]any{"name": "Deep focus", "revision": p.Revision, "capture": true, "memory": false, "prefs": map[string]any{"follow": false}})
	renamed := decode[profile.Profile](t, rr)
	if rr.Code != http.StatusOK || renamed.Name != "Deep focus" || renamed.Data.Memory != nil || renamed.Revision != p.Revision+1 {
		t.Fatalf("rename = %d %+v", rr.Code, renamed)
	}
	if rr = doJSON(t, mux, "PUT", "/api/profiles/"+strconv.FormatInt(p.ID, 10), map[string]any{"name": "Again", "revision": p.Revision}); rr.Code != http.StatusConflict {
		t.Fatalf("stale rename = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "DELETE", "/api/profiles/"+strconv.FormatInt(p.ID, 10)+"?revision="+strconv.FormatInt(renamed.Revision, 10), nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rr.Code)
	}
	list = decode[profilesResponse](t, doJSON(t, mux, "GET", "/api/profiles", nil))
	if len(list.Profiles) != 0 || list.ActiveID != 0 {
		t.Fatalf("after delete = %+v", list)
	}
}

func presetOf(name, key, value string) mods.Preset {
	return mods.Preset{Name: name, Vars: map[string]string{key: value}}
}
