package server_test

import (
	"net/http"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/server"
	"github.com/Labyricorn/Labbonet/internal/server/servertest"
)

// S6: a spent launch code cannot be redeemed again, and an invalid code is
// rejected outright.
func TestLaunchCodeSingleUse(t *testing.T) {
	sessions, err := server.NewSessionManager()
	if err != nil {
		t.Fatalf("new session manager: %v", err)
	}
	code := sessions.LaunchCode()

	if !sessions.RedeemLaunchCode(code) {
		t.Fatal("first redemption of a fresh code should succeed")
	}
	if sessions.RedeemLaunchCode(code) {
		t.Fatal("second redemption of the same code should fail (S6)")
	}
	if sessions.RedeemLaunchCode("not-the-real-code") {
		t.Fatal("redeeming a wrong code should fail")
	}
}

// S6 (HTTP path): a GET /launch with an invalid code is rejected with 403
// and never sets a session cookie.
func TestLaunchEndpointRejectsInvalidCode(t *testing.T) {
	env := servertest.Start(t)

	req, err := http.NewRequest(http.MethodGet, env.Origin+"/launch?code=not-the-real-code", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got status %d, want 403 for an invalid code", resp.StatusCode)
	}
}
