package egress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestCheckDialAddress(t *testing.T) {
	cases := []struct {
		name    string
		address string
		class   string
		wantErr bool
	}{
		{"loopback declared local", "127.0.0.1:80", ClassLocal, false},
		{"loopback declared lan mismatches", "127.0.0.1:80", ClassLAN, true},
		{"RFC1918 declared lan", "10.0.0.5:80", ClassLAN, false},
		{"RFC1918 declared local mismatches", "10.0.0.5:80", ClassLocal, true},
		{"public declared external", "8.8.8.8:443", ClassExternal, false},
		{"public declared local mismatches", "8.8.8.8:443", ClassLocal, true},
		{"IPv4 metadata always refused", "169.254.169.254:80", ClassLAN, true},
		{"IPv4 metadata refused even if declared external", "169.254.169.254:80", ClassExternal, true},
		{"IPv6 metadata always refused", "[fd00:ec2::254]:80", ClassLAN, true},
		{"non-metadata link-local is lan", "[fe80::1]:80", ClassLAN, false},
		{"unparseable address", "not-an-address", ClassLocal, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkDialAddress(c.address, c.class)
			if (err != nil) != c.wantErr {
				t.Fatalf("checkDialAddress(%q, %q) = %v, wantErr %v", c.address, c.class, err, c.wantErr)
			}
		})
	}
}

// S8: a server that redirects is refused, not followed.
func TestDo_RefusesRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/models", http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient()
	target := Target{BaseURL: mustParseURL(t, srv.URL), PrivacyClass: ClassLocal}
	_, err := c.Do(context.Background(), target, http.MethodGet, "/v1/models", nil)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("Do() err = %v, want a redirect refusal", err)
	}
}

// A request to a path outside the fixed allowlist is refused before
// any network activity.
func TestDo_RefusesPathOutsideAllowlist(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := NewClient()
	target := Target{BaseURL: mustParseURL(t, srv.URL), PrivacyClass: ClassLocal}
	_, err := c.Do(context.Background(), target, http.MethodGet, "/v1/models/evil", nil)
	if err == nil {
		t.Fatal("Do() with a disallowed path did not error")
	}
	if called {
		t.Fatal("server was contacted despite a disallowed path")
	}
}

// S9 analog: the real dialer, not just the table test, refuses a
// connection when the live resolved address does not match the
// endpoint's declared privacy class.
func TestDo_DialGuardRefusesClassMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient()
	// srv.URL is a loopback address; declaring it "lan" must be refused.
	target := Target{BaseURL: mustParseURL(t, srv.URL), PrivacyClass: ClassLAN}
	_, err := c.Do(context.Background(), target, http.MethodGet, "/v1/models", nil)
	if err == nil {
		t.Fatal("Do() with a mismatched privacy class did not error")
	}

	// The correctly declared class still works end to end.
	target.PrivacyClass = ClassLocal
	resp, err := c.Do(context.Background(), target, http.MethodGet, "/v1/models", nil)
	if err != nil {
		t.Fatalf("Do() with the correct class: %v", err)
	}
	resp.Body.Close()
}

// S11: a credential set for one endpoint must never reach another
// endpoint's request.
func TestDo_CredentialScopedToItsOwnTarget(t *testing.T) {
	var gotAuthA, gotAuthB string
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthA = r.Header.Get("Authorization")
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthB = r.Header.Get("Authorization")
	}))
	defer srvB.Close()

	c := NewClient()
	targetA := Target{BaseURL: mustParseURL(t, srvA.URL), PrivacyClass: ClassLocal, Credential: "secret-a"}
	targetB := Target{BaseURL: mustParseURL(t, srvB.URL), PrivacyClass: ClassLocal}

	respA, err := c.Do(context.Background(), targetA, http.MethodGet, "/v1/models", nil)
	if err != nil {
		t.Fatalf("Do targetA: %v", err)
	}
	respA.Body.Close()

	respB, err := c.Do(context.Background(), targetB, http.MethodGet, "/v1/models", nil)
	if err != nil {
		t.Fatalf("Do targetB: %v", err)
	}
	respB.Body.Close()

	if gotAuthA != "Bearer secret-a" {
		t.Fatalf("endpoint A Authorization = %q, want %q", gotAuthA, "Bearer secret-a")
	}
	if gotAuthB != "" {
		t.Fatalf("endpoint B Authorization = %q, want empty (no credential set)", gotAuthB)
	}
}

func TestDoLimited_EnforcesSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	c := NewClient()
	c.MaxResponseBytes = 10
	target := Target{BaseURL: mustParseURL(t, srv.URL), PrivacyClass: ClassLocal}
	_, _, err := c.DoLimited(context.Background(), target, http.MethodGet, "/v1/models", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("DoLimited() err = %v, want a size-limit error", err)
	}
}

func TestDoLimited_ReturnsBodyUnderLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := NewClient()
	target := Target{BaseURL: mustParseURL(t, srv.URL), PrivacyClass: ClassLocal}
	_, data, err := c.DoLimited(context.Background(), target, http.MethodGet, "/v1/models", nil)
	if err != nil {
		t.Fatalf("DoLimited: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("data = %q, want %q", data, "ok")
	}
}
