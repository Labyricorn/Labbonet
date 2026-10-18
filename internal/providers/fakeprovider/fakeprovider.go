// Package fakeprovider is a scriptable OpenAI-compatible fake server
// for provider adapter and egress tests (PHASE1-PLAN.md section 2). It
// never talks to a real provider; tests script exactly the response
// bytes an upstream server would send, including Strata's keep-alive
// comments, usage/timings, and error shapes.
package fakeprovider

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Script configures one fake provider's responses. Every field is
// optional; an unset response path answers 404.
type Script struct {
	HealthStatus int
	HealthBody   string

	// ModelsPath selects which request path answers with ModelsBody:
	// "/v1/models" (Strata, generic) or "/api/v0/models" (LM Studio).
	// The other models path always 404s.
	ModelsPath   string
	ModelsStatus int
	ModelsBody   string

	ChatStatus int // 0 means 200
	// ChatSSE is the exact response body for a 200 chat completion:
	// full control over "data:" framing, keep-alive comments, usage,
	// timings and "[DONE]", exactly as an upstream server would send
	// them.
	ChatSSE string
	// ChatErrorBody is the response body when ChatStatus is not 200
	// (or 0).
	ChatErrorBody string
	// ChatFirstByteDelay, if set, is slept before any byte of the chat
	// response is written (a slow/cold-load first byte).
	ChatFirstByteDelay time.Duration
	// ChatDropAfterBytes, if set and less than len(ChatSSE), truncates
	// the written body there and returns without sending "[DONE]" (a
	// mid-stream drop).
	ChatDropAfterBytes int

	// RedirectPath, if set, makes that one request path answer with a
	// 302 to RedirectLocation instead of its normal response.
	RedirectPath     string
	RedirectLocation string
}

// Server is a running fake provider.
type Server struct {
	*httptest.Server

	mu       sync.Mutex
	script   Script
	lastAuth string
}

// New starts a fake provider server with the given script.
func New(script Script) *Server {
	s := &Server{script: script}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.handleModels("/v1/models"))
	mux.HandleFunc("/api/v0/models", s.handleModels("/api/v0/models"))
	mux.HandleFunc("/v1/chat/completions", s.handleChat)
	s.Server = httptest.NewServer(mux)
	return s
}

// LastAuthHeader returns the Authorization header of the most recent
// request this server received (empty if none was set), for
// credential-scoping tests.
func (s *Server) LastAuthHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAuth
}

func (s *Server) recordAuth(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAuth = r.Header.Get("Authorization")
}

func (s *Server) maybeRedirect(w http.ResponseWriter, r *http.Request) bool {
	if s.script.RedirectPath != "" && r.URL.Path == s.script.RedirectPath {
		http.Redirect(w, r, s.script.RedirectLocation, http.StatusFound)
		return true
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.recordAuth(r)
	if s.maybeRedirect(w, r) {
		return
	}
	status := s.script.HealthStatus
	if status == 0 {
		status = http.StatusNotFound
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(s.script.HealthBody))
}

func (s *Server) handleModels(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.recordAuth(r)
		if s.maybeRedirect(w, r) {
			return
		}
		if s.script.ModelsPath != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		status := s.script.ModelsStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(s.script.ModelsBody))
	}
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	s.recordAuth(r)
	if s.maybeRedirect(w, r) {
		return
	}
	status := s.script.ChatStatus
	if status == 0 {
		status = http.StatusOK
	}
	if status != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(s.script.ChatErrorBody))
		return
	}

	if s.script.ChatFirstByteDelay > 0 {
		time.Sleep(s.script.ChatFirstByteDelay)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	body := s.script.ChatSSE
	if s.script.ChatDropAfterBytes > 0 && s.script.ChatDropAfterBytes < len(body) {
		body = body[:s.script.ChatDropAfterBytes]
	}
	w.Write([]byte(body))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
