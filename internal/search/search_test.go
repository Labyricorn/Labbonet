package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
)

func fake(t *testing.T, h http.HandlerFunc) *egress.SearchClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return egress.NewSearchClientFor(u, egress.ClassLocal)
}

func TestQuery(t *testing.T) {
	long := strings.Repeat("word ", 200)
	for name, c := range map[string]struct{ in, want string }{
		"plain":       {"  what   is\n a tide? ", "what is a tide?"},
		"attachment":  {"summarise this\n\n[Attached file: a.txt]\n```\nsecret\n```", "summarise this"},
		"control":     {"a\x00b‮c d", "abc d"},
		"macro tags":  {"why tides? {{WRAP_UP}} {{user}} please", "why tides? please"},
		"empty":       {" \n\t ", ""},
		"only attach": {"\n\n[Attached file: a.txt]\nx", ""},
	} {
		if got := Query(c.in); got != c.want {
			t.Errorf("%s: Query = %q, want %q", name, got, c.want)
		}
	}
	if got := Query(long); len([]rune(got)) > MaxQueryRunes || len([]rune(got)) < MaxQueryRunes-1 {
		t.Errorf("long query has %d runes, want about %d", len([]rune(got)), MaxQueryRunes)
	}
}

func TestParseDropsUnsafeAndBounds(t *testing.T) {
	body := `{"results":[
		{"title":"ok","url":"https://example.org/a","content":"hello\nworld"},
		{"title":"js","url":"javascript:alert(1)","content":"x"},
		{"title":"data","url":"data:text/html,x","content":"x"},
		{"title":"creds","url":"https://user:pw@example.org/","content":"x"},
		{"title":"nohost","url":"https:///x","content":"x"},
		{"title":"","url":"http://example.net/b","content":"` + strings.Repeat("z", 5000) + `"},
		{"title":"third","url":"https://example.org/c","content":"c"}]}`
	got, err := Parse([]byte(body), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].URL != "https://example.org/a" || got[1].URL != "http://example.net/b" {
		t.Fatalf("results = %+v", got)
	}
	if got[0].Content != "hello world" {
		t.Errorf("content not collapsed: %q", got[0].Content)
	}
	if got[1].Title != "http://example.net/b" || len([]rune(got[1].Content)) != MaxContentRunes {
		t.Errorf("title fallback / content bound wrong: %q %d", got[1].Title, len([]rune(got[1].Content)))
	}
	for _, bad := range []string{`not json`, `{"error":"nope"}`, `{"error":{"message":"x"}}`} {
		if _, err := Parse([]byte(bad), 5); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	if got, err := Parse([]byte(`{}`), 5); err != nil || len(got) != 0 {
		t.Errorf("empty response = %v, %v", got, err)
	}
}

func TestRunSendsKeyedRequestToFixedPath(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	var gotBody map[string]any
	client := fake(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"results":[{"title":"t","url":"https://example.org/","content":"c"}]}`))
	})
	res, err := Run(context.Background(), client, "k-123", "tides", 3)
	if err != nil || len(res) != 1 {
		t.Fatalf("Run = %v, %v", res, err)
	}
	if gotAuth != "Bearer k-123" || gotPath != egress.SearchPath || gotMethod != "POST" {
		t.Errorf("request = %s %s auth %q", gotMethod, gotPath, gotAuth)
	}
	if gotBody["query"] != "tides" || gotBody["max_results"] != float64(3) {
		t.Errorf("body = %v", gotBody)
	}
}

func TestRunErrorsNeverMentionKey(t *testing.T) {
	const key = "sk-very-secret-key"
	for name, c := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"rejected":   {401, `{"error":"` + key + `"}`, "rejected the API key"},
		"forbidden":  {403, key, "rejected the API key"},
		"throttled":  {429, key, "rate limiting"},
		"server":     {503, key, "HTTP 503"},
		"not json":   {200, "<html>" + key, "not valid JSON"},
		"error body": {200, `{"error":"` + key + `"}`, "reported an error"},
	} {
		client := fake(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		})
		_, err := Run(context.Background(), client, key, "q", 2)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), key) {
			t.Errorf("%s: error leaks the key: %v", name, err)
		}
	}
	if _, err := Run(context.Background(), nil, "", "q", 2); err != ErrNoKey {
		t.Errorf("no key = %v", err)
	}
	if _, err := Run(context.Background(), nil, "k", "", 2); err == nil {
		t.Error("empty query accepted")
	}
}

func TestRunRefusesRedirectsAndHugeBodies(t *testing.T) {
	client := fake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	})
	if _, err := Run(context.Background(), client, "k", "q", 2); err == nil {
		t.Error("redirect followed")
	}
	client = fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[],"pad":"` + strings.Repeat("x", 2<<20) + `"}`))
	})
	if _, err := Run(context.Background(), client, "k", "q", 2); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("oversized body = %v", err)
	}
}

// The production client is public-addresses-only: a search host that resolves to loopback
// (or a LAN address) is refused before any packet is sent.
func TestExternalClassRefusesLoopback(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	client := egress.NewSearchClientFor(u, egress.ClassExternal)
	if _, err := Run(context.Background(), client, "k", "q", 2); err == nil || called {
		t.Errorf("loopback reached under external class: err=%v called=%v", err, called)
	}
}

func TestSettingsValidate(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, -1, 11} {
		if err := (Settings{MaxResults: n}).Validate(); !IsSettingError(err) {
			t.Errorf("MaxResults %d accepted", n)
		}
	}
}
