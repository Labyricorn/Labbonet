package server

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLog_RecordsMethodPathStatus(t *testing.T) {
	var buf strings.Builder
	logger := log.New(&buf, "", 0)

	handler := AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/endpoints", nil)
	req.Header.Set("Authorization", "Bearer sk-should-not-appear")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	out := buf.String()
	if !strings.Contains(out, "GET") || !strings.Contains(out, "/api/endpoints") || !strings.Contains(out, "418") {
		t.Fatalf("log output = %q, want method, path and status", out)
	}
	if strings.Contains(out, "sk-should-not-appear") {
		t.Fatalf("log output leaked a header value: %q", out)
	}
}

func TestAccessLog_NilLoggerIsNoop(t *testing.T) {
	called := false
	handler := AccessLog(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("handler not invoked correctly: called=%v code=%d", called, rr.Code)
	}
}
