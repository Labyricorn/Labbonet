// Package servertest starts a Labbonet server on a random loopback port
// against a temp data directory, for use by package tests.
package servertest

import (
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/api"
	"github.com/Labyricorn/Labbonet/internal/server"
)

// Env is a running test server plus a client that already holds a valid
// session cookie.
type Env struct {
	Origin string
	Client *http.Client
}

// Start boots a server on a random loopback port, authenticates a client
// through the launch-code flow, and registers cleanup to shut it down.
func Start(t *testing.T) *Env {
	t.Helper()

	dataDir := t.TempDir()
	lock, err := server.AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	t.Cleanup(func() { lock.Release() })

	sessions, err := server.NewSessionManager()
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv, err := server.New(server.Config{
		Host:     "127.0.0.1",
		Port:     port,
		Sessions: sessions,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srv.Handle("GET /api/health", http.HandlerFunc(api.Health))

	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	origin := fmt.Sprintf("http://127.0.0.1:%d", port)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	// Don't follow the redirect to "/": in tests the server has no
	// frontend configured, so "/" has no handler and would 404. The
	// redirect itself is what proves the launch code was accepted.
	noRedirect := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	launchURL := fmt.Sprintf("%s/launch?code=%s", origin, sessions.LaunchCode())
	resp, err := noRedirect.Get(launchURL)
	if err != nil {
		t.Fatalf("redeem launch code: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("redeem launch code: status %d, want 303", resp.StatusCode)
	}

	u, _ := url.Parse(origin)
	if len(jar.Cookies(u)) == 0 {
		t.Fatalf("launch flow did not set a session cookie")
	}

	return &Env{Origin: origin, Client: client}
}
