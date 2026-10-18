package server

import (
	"log"
	"net/http"
	"time"
)

// AccessLog wraps next with a logger that records method, path and
// status for every request and nothing else: never headers, never
// query strings, never bodies. Request metadata logging can therefore
// never leak conversation content or a credential, even if a future
// caller passes headers through RedactHeaders incorrectly (SECURITY-
// MODEL.md section 7).
func AccessLog(logger *log.Logger, next http.Handler) http.Handler {
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
