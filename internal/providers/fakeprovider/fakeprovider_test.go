package fakeprovider

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestServer_HealthAndModelsAndAuthCapture(t *testing.T) {
	srv := New(Script{
		HealthStatus: 200,
		HealthBody:   `{"service":"strata"}`,
		ModelsPath:   "/v1/models",
		ModelsBody:   `{"data":[]}`,
	})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != `{"service":"strata"}` {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if srv.LastAuthHeader() != "Bearer test-token" {
		t.Fatalf("LastAuthHeader = %q", srv.LastAuthHeader())
	}

	resp2, err := http.Get(srv.URL + "/api/v0/models")
	if err != nil {
		t.Fatalf("GET /api/v0/models: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (wrong models path for this script)", resp2.StatusCode)
	}
}

func TestServer_ChatDropAfterBytesTruncatesBody(t *testing.T) {
	full := "data: {\"a\":1}\n\ndata: [DONE]\n\n"
	srv := New(Script{ChatSSE: full, ChatDropAfterBytes: 5})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 5 {
		t.Fatalf("body len = %d, want 5 (truncated)", len(body))
	}
}

func TestServer_ChatFirstByteDelay(t *testing.T) {
	srv := New(Script{ChatSSE: "data: [DONE]\n\n", ChatFirstByteDelay: 30 * time.Millisecond})
	defer srv.Close()

	start := time.Now()
	resp, err := http.Post(srv.URL+"/v1/chat/completions", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("response arrived before the configured first-byte delay")
	}
}

func TestServer_Redirect(t *testing.T) {
	srv := New(Script{RedirectPath: "/v1/models", RedirectLocation: "/v1/models/elsewhere"})
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
}
