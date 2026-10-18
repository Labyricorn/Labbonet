package server

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"
)

// Config holds the parameters needed to build a Server.
type Config struct {
	Host string // must be "127.0.0.1"
	Port int

	// Frontend serves the static site. Pass an fs.FS rooted at the
	// frontend directory (embedded, or os.DirFS for --frontend-dir).
	Frontend fs.FS

	Sessions *SessionManager
}

// Server is the Labbonet HTTP server: a loopback listener with the full
// browser-to-backend security middleware chain in front of the static
// frontend and the /api handlers.
type Server struct {
	cfg      Config
	mux      *http.ServeMux
	handler  http.Handler
	sessions *SessionManager
	origin   string
	httpSrv  *http.Server
}

// New builds a Server. Call Handle to register API routes before Listen.
func New(cfg Config) (*Server, error) {
	if cfg.Host != "127.0.0.1" {
		return nil, fmt.Errorf("--host must be 127.0.0.1; got %q (Labbonet never listens on a non-loopback address)", cfg.Host)
	}

	s := &Server{
		cfg:      cfg,
		mux:      http.NewServeMux(),
		sessions: cfg.Sessions,
		origin:   fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
	}

	s.mux.HandleFunc("GET /launch", s.handleLaunch)
	if cfg.Frontend != nil {
		s.mux.Handle("/", http.FileServerFS(cfg.Frontend))
	}

	allowedHosts := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", cfg.Port): true,
		fmt.Sprintf("localhost:%d", cfg.Port): true,
	}

	var root http.Handler = s.mux
	root = s.apiGate(root)
	root = hostAllowlist(allowedHosts, root)
	root = withSecurityHeaders(root)
	s.handler = root

	return s, nil
}

// apiGate applies the Origin/header/session checks only to /api paths,
// leaving static asset and /launch requests to the outer checks only.
func (s *Server) apiGate(next http.Handler) http.Handler {
	gated := apiOrigin(s.origin, s.sessions, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			gated.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Handle registers an API handler under /api. Call before Listen.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if !s.sessions.RedeemLaunchCode(code) {
		http.Error(w, "invalid or expired launch code", http.StatusForbidden)
		return
	}
	s.sessions.SetSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Handler returns the fully wrapped root handler (used by tests that want
// to drive the server via httptest without a real listener).
func (s *Server) Handler() http.Handler {
	return s.handler
}

// LaunchURL returns the one-time URL the user opens to authenticate the
// browser session.
func (s *Server) LaunchURL() string {
	return fmt.Sprintf("%s/launch?code=%s", s.origin, s.sessions.LaunchCode())
}

// Listen binds the loopback listener. Call before Serve.
func (s *Server) Listen() (net.Listener, error) {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w (is another program already using this port?)", addr, err)
	}
	return ln, nil
}

// Serve runs the HTTP server on the given listener until Shutdown is
// called.
func (s *Server) Serve(ln net.Listener) error {
	s.httpSrv = &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	err := s.httpSrv.Serve(ln)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown stops accepting new requests and waits for in-flight ones to
// finish, bounded by ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

// DirFrontend is a convenience wrapper so callers don't need to import
// os directly just to build an fs.FS for --frontend-dir.
func DirFrontend(dir string) fs.FS {
	return os.DirFS(dir)
}
