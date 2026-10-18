package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers/fakeprovider"
	"github.com/Labyricorn/Labbonet/internal/secrets"
	"github.com/Labyricorn/Labbonet/internal/store"
	"github.com/Labyricorn/Labbonet/internal/store/storetest"
)

func newModsMux(t *testing.T, searchURL string) (*http.ServeMux, *store.Store) {
	t.Helper()
	st := storetest.New(t)
	m := &Mods{Store: st, Protector: secrets.NewProtector()}
	if searchURL != "" {
		u, _ := url.Parse(searchURL)
		m.Search = egress.NewSearchClientFor(u, egress.ClassLocal)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/macros", m.ListMacros)
	mux.HandleFunc("POST /api/macros", m.SaveMacro)
	mux.HandleFunc("POST /api/macros/preview", m.PreviewMacros)
	mux.HandleFunc("PUT /api/macros/{id}", m.SaveMacro)
	mux.HandleFunc("DELETE /api/macros/{id}", m.DeleteMacro)
	mux.HandleFunc("GET /api/appearance", m.GetAppearance)
	mux.HandleFunc("PUT /api/appearance", m.PutAppearance)
	mux.HandleFunc("GET /api/search/settings", m.GetSearchSettings)
	mux.HandleFunc("PUT /api/search/settings", m.PutSearchSettings)
	mux.HandleFunc("PUT /api/search/credential", m.PutSearchKey)
	mux.HandleFunc("DELETE /api/search/credential", m.DeleteSearchKey)
	mux.HandleFunc("POST /api/search/test", m.TestSearch)
	mux.HandleFunc("GET /api/conversations/{id}/search", m.SearchRecords)
	return mux, st
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return out
}

func TestMacroAPI(t *testing.T) {
	mux, _ := newModsMux(t, "")
	rr := doJSON(t, mux, "POST", "/api/macros", map[string]any{"trigger": "wrap_up", "text": "please conclude"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body)
	}
	m := decode[struct {
		ID       int64 `json:"id"`
		Revision int64 `json:"revision"`
	}](t, rr)
	id := strconv.FormatInt(m.ID, 10)

	for _, c := range []struct {
		name         string
		method, path string
		body         any
		want         int
	}{
		{"duplicate", "POST", "/api/macros", map[string]any{"trigger": "WRAP_UP", "text": "x"}, 400},
		{"reserved", "POST", "/api/macros", map[string]any{"trigger": "user", "text": "x"}, 400},
		{"bad trigger", "POST", "/api/macros", map[string]any{"trigger": "a b", "text": "x"}, 400},
		{"cycle", "POST", "/api/macros", map[string]any{"trigger": "loop", "text": "{{loop}}"}, 400},
		{"edit w/o rev", "PUT", "/api/macros/" + id, map[string]any{"trigger": "wrap_up", "text": "x"}, 400},
		{"edit stale rev", "PUT", "/api/macros/" + id, map[string]any{"trigger": "wrap_up", "text": "x", "revision": 9}, 409},
		{"edit ok", "PUT", "/api/macros/" + id, map[string]any{"trigger": "wrap_up", "text": "finish up", "revision": m.Revision}, 200},
		{"edit missing", "PUT", "/api/macros/999", map[string]any{"trigger": "ghost", "text": "x", "revision": 1}, 404},
		{"bad id", "PUT", "/api/macros/abc", map[string]any{"trigger": "a", "text": "x", "revision": 1}, 400},
		{"delete bad rev", "DELETE", "/api/macros/" + id + "?revision=x", nil, 400},
		{"delete stale", "DELETE", "/api/macros/" + id + "?revision=1", nil, 409},
		{"delete missing", "DELETE", "/api/macros/999", nil, 404},
		{"preview", "POST", "/api/macros/preview", map[string]any{"text": "do {{WRAP_UP}} {{char}}"}, 200},
		{"preview invalid", "POST", "/api/macros/preview", map[string]any{"text": strings.Repeat("x", 20000)}, 400},
	} {
		if rr := doJSON(t, mux, c.method, c.path, c.body); rr.Code != c.want {
			t.Errorf("%s: %s %s = %d %s, want %d", c.name, c.method, c.path, rr.Code, strings.TrimSpace(rr.Body.String()), c.want)
		}
	}
	rr = doJSON(t, mux, "POST", "/api/macros/preview", map[string]any{"text": "do {{WRAP_UP}} {{char}}"})
	if got := decode[map[string]string](t, rr)["text"]; got != "do finish up Character" {
		t.Errorf("preview = %q", got)
	}
	list := decode[struct {
		Macros   []map[string]any `json:"macros"`
		Reserved []string         `json:"reserved"`
	}](t, doJSON(t, mux, "GET", "/api/macros", nil))
	if len(list.Macros) != 1 || len(list.Reserved) == 0 {
		t.Errorf("list = %+v", list)
	}
	if rr := doJSON(t, mux, "DELETE", "/api/macros/"+id+"?revision=2", nil); rr.Code != http.StatusNoContent {
		t.Errorf("delete = %d %s", rr.Code, rr.Body)
	}
}

func TestAppearanceAPI(t *testing.T) {
	mux, _ := newModsMux(t, "")
	cur := decode[map[string]any](t, doJSON(t, mux, "GET", "/api/appearance", nil))
	if cur["revision"] != float64(1) || len(cur["builtin"].([]any)) == 0 || len(cur["adjustable"].([]any)) == 0 {
		t.Fatalf("get = %v", cur)
	}
	for name, c := range map[string]struct {
		body map[string]any
		want int
	}{
		"no revision":  {map[string]any{"active": ""}, 400},
		"unknown":      {map[string]any{"revision": 1, "active": "nope"}, 400},
		"url value":    {map[string]any{"revision": 1, "presets": []any{map[string]any{"name": "x", "vars": map[string]any{"--black": "url(http://example.org/a)"}}}}, 400},
		"bad property": {map[string]any{"revision": 1, "presets": []any{map[string]any{"name": "x", "vars": map[string]any{"background": "#000000"}}}}, 400},
		"stale":        {map[string]any{"revision": 7, "active": "amber"}, 409},
		"ok":           {map[string]any{"revision": 1, "active": "amber"}, 200},
	} {
		if rr := doJSON(t, mux, "PUT", "/api/appearance", c.body); rr.Code != c.want {
			t.Errorf("%s = %d %s, want %d", name, rr.Code, strings.TrimSpace(rr.Body.String()), c.want)
		}
	}
	got := decode[map[string]any](t, doJSON(t, mux, "GET", "/api/appearance", nil))
	if eff := got["effective"].(map[string]any); eff["--cyan-bright"] == nil {
		t.Errorf("effective values missing for the active preset: %v", got)
	}
}

func TestSearchSettingsAndKeyAreNeverEchoed(t *testing.T) {
	const key = "sk-api-test-key-xyz"
	mux, st := newModsMux(t, "")
	if _, err := secrets.NewProtector().Protect("probe"); err != nil {
		t.Skipf("credential protection unavailable: %v", err)
	}
	cur := decode[struct {
		Revision      int64  `json:"revision"`
		Enabled       bool   `json:"enabled"`
		CredentialSet bool   `json:"credential_set"`
		Service       string `json:"service"`
	}](t, doJSON(t, mux, "GET", "/api/search/settings", nil))
	if cur.Enabled || cur.CredentialSet || cur.Service != "ollama.com" {
		t.Fatalf("defaults = %+v (search must start off with no key)", cur)
	}
	for name, v := range map[string]string{"empty": "", "space": "a b", "newline": "a\nb", "long": strings.Repeat("k", 600)} {
		if rr := doJSON(t, mux, "PUT", "/api/search/credential", map[string]any{"value": v}); rr.Code != 400 {
			t.Errorf("%s key = %d", name, rr.Code)
		}
	}
	rr := doJSON(t, mux, "PUT", "/api/search/credential", map[string]any{"value": key})
	if rr.Code != 200 || strings.Contains(rr.Body.String(), key) || !strings.Contains(rr.Body.String(), `"credential_set":true`) {
		t.Fatalf("set key = %d %s", rr.Code, rr.Body)
	}
	if blob, _ := st.SearchCredentialBlob(t.Context()); len(blob) == 0 || strings.Contains(string(blob), key) {
		t.Errorf("stored blob is empty or plaintext: %q", blob)
	}
	rr = doJSON(t, mux, "PUT", "/api/search/settings", map[string]any{"revision": cur.Revision, "enabled": true, "max_results": 3})
	if rr.Code != 200 || strings.Contains(rr.Body.String(), key) || !strings.Contains(rr.Body.String(), `"enabled":true`) {
		t.Fatalf("settings = %d %s", rr.Code, rr.Body)
	}
	if rr = doJSON(t, mux, "PUT", "/api/search/settings", map[string]any{"revision": cur.Revision, "enabled": true, "max_results": 3}); rr.Code != 409 {
		t.Errorf("stale settings = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "PUT", "/api/search/settings", map[string]any{"revision": 2, "enabled": true, "max_results": 50}); rr.Code != 400 {
		t.Errorf("out of range = %d", rr.Code)
	}
	if rr = doJSON(t, mux, "PUT", "/api/search/settings", map[string]any{"enabled": true, "max_results": 3}); rr.Code != 400 {
		t.Errorf("missing revision = %d", rr.Code)
	}
	rr = doJSON(t, mux, "DELETE", "/api/search/credential", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"credential_set":false`) {
		t.Errorf("clear key = %d %s", rr.Code, rr.Body)
	}
}

func TestSearchConnectionTestSendsOnlyTheFixedPhrase(t *testing.T) {
	const key = "sk-test-conn-key"
	if _, err := secrets.NewProtector().Protect("probe"); err != nil {
		t.Skipf("credential protection unavailable: %v", err)
	}
	var gotQuery, gotAuth string
	status := 200
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotQuery, gotAuth = in.Query, r.Header.Get("Authorization")
		w.WriteHeader(status)
		w.Write([]byte(`{"results":[{"title":"t","url":"https://example.org/","content":"c"}]}`))
	}))
	defer svc.Close()
	mux, _ := newModsMux(t, svc.URL)

	if rr := doJSON(t, mux, "POST", "/api/search/test", nil); rr.Code != 400 || gotAuth != "" {
		t.Fatalf("test without a key = %d (service contacted: %v)", rr.Code, gotAuth != "")
	}
	doJSON(t, mux, "PUT", "/api/search/credential", map[string]any{"value": key})
	rr := doJSON(t, mux, "POST", "/api/search/test", nil)
	if rr.Code != 200 || gotQuery != "labbonet connection test" || gotAuth != "Bearer "+key {
		t.Fatalf("test = %d %s query %q auth %q", rr.Code, rr.Body, gotQuery, gotAuth)
	}
	status = 401
	rr = doJSON(t, mux, "POST", "/api/search/test", nil)
	if rr.Code != http.StatusBadGateway || strings.Contains(rr.Body.String(), key) {
		t.Errorf("rejected key = %d %s", rr.Code, rr.Body)
	}
}

func TestSearchRecordsEndpoint(t *testing.T) {
	mux, st := newModsMux(t, "")
	conv, err := st.CreateConversation(t.Context(), "T", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.AddSearchRecord(t.Context(), conv.ID, nil, map[string]string{"query": "q"}); err != nil {
		t.Fatal(err)
	}
	rr := doJSON(t, mux, "GET", "/api/conversations/"+strconv.FormatInt(conv.ID, 10)+"/search", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"query":"q"`) {
		t.Errorf("records = %d %s", rr.Code, rr.Body)
	}
	if rr = doJSON(t, mux, "GET", "/api/conversations/999/search", nil); rr.Code != 404 {
		t.Errorf("missing conversation = %d", rr.Code)
	}
}

// Asking for search while it is off is a refusal the user can read, and nothing is stored.
func TestGenerateRefusesWebSearchWhenItIsOff(t *testing.T) {
	srv := fakeprovider.New(fakeprovider.Script{ChatSSE: generateTestSSE, ModelsPath: "/v1/models", ModelsBody: `{"data":[]}`})
	defer srv.Close()
	mux, st, _, convID := newGenerateHandler(t, srv.URL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+strconv.FormatInt(convID, 10)+"/generate",
		strings.NewReader(`{"content":"hi","web_search":true}`))
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "web search") {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body)
	}
	if msgs, _ := st.ListMessages(t.Context(), convID); len(msgs) != 0 {
		t.Errorf("refused send stored %d messages", len(msgs))
	}
}
