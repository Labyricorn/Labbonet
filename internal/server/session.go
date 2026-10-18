package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "labbonet_session"
	launchCodeTTL     = 2 * time.Minute
)

// SessionManager issues a single launch code at startup, exchanges it once
// for a long-lived session cookie value, and validates that cookie value on
// every /api request.
type SessionManager struct {
	mu sync.Mutex

	launchCode   string
	launchExpiry time.Time
	launchSpent  bool

	sessionToken string
}

// NewSessionManager generates the 256-bit session token and the one-time
// launch code.
func NewSessionManager() (*SessionManager, error) {
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	code, err := randomURLSafe(32)
	if err != nil {
		return nil, err
	}
	return &SessionManager{
		launchCode:   code,
		launchExpiry: time.Now().Add(launchCodeTTL),
		sessionToken: token,
	}, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// LaunchCode returns the current one-time code, for building the printed
// launch URL.
func (m *SessionManager) LaunchCode() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.launchCode
}

// RedeemLaunchCode validates and consumes the launch code. It returns false
// if the code is wrong, already spent, or expired.
func (m *SessionManager) RedeemLaunchCode(code string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.launchSpent {
		return false
	}
	if time.Now().After(m.launchExpiry) {
		return false
	}
	if !constantTimeEqual(code, m.launchCode) {
		return false
	}
	m.launchSpent = true
	return true
}

// ValidSessionCookie reports whether the request carries the correct
// session cookie value.
func (m *SessionManager) ValidSessionCookie(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return constantTimeEqual(c.Value, m.sessionToken)
}

// SetSessionCookie attaches the session cookie to the response.
func (m *SessionManager) SetSessionCookie(w http.ResponseWriter) {
	m.mu.Lock()
	token := m.sessionToken
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
