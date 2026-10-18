package server_test

import (
	"net/http"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/server/servertest"
)

// S1: a request with a Host header outside the allowlist gets 421 and no
// handler runs.
func TestHostAllowlistRejectsUnknownHost(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example:4815"
	req.Header.Set("X-Labbonet", "1")

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("got status %d, want 421", resp.StatusCode)
	}
}

// S2: a cross-origin POST carrying the (stolen) session cookie is still
// rejected because Origin does not match.
func TestOriginCheckRejectsCrossOrigin(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Labbonet", "1")
	req.Header.Set("Origin", "https://evil.example")

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", resp.StatusCode)
	}
}

// S2b: Sec-Fetch-Site other than same-origin is also rejected.
func TestSecFetchSiteCheckRejectsCrossSite(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Labbonet", "1")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", resp.StatusCode)
	}
}

// S3: a request to /api without X-Labbonet is rejected even with a valid
// session cookie.
func TestMissingLabbonetHeaderRejected(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got status %d, want 403", resp.StatusCode)
	}
}

// S4: a CORS preflight to /api/* gets no Access-Control-Allow-* headers.
func TestPreflightGetsNoCORSHeaders(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodOptions, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	for name := range resp.Header {
		if len(name) >= len("Access-Control-") && name[:len("Access-Control-")] == "Access-Control-" {
			t.Fatalf("unexpected CORS header %q in response", name)
		}
	}
}

// S5: a request to /api without the session cookie is rejected.
func TestMissingSessionCookieRejected(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Labbonet", "1")
	// Deliberately use a bare client with no cookie jar.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", resp.StatusCode)
	}
}

// Sanity check: the full, correctly-authenticated path succeeds.
func TestHealthSucceedsWithValidSession(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Labbonet", "1")

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}
}

// Security headers are present on every response, including an error
// response from the allowlist check.
func TestSecurityHeadersPresentOnErrorResponses(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example:4815"

	resp, err := env.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy header")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing or wrong X-Content-Type-Options header")
	}
}
