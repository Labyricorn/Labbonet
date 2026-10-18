package server

import (
	"net/http"
	"testing"
)

func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-secret")
	h.Set("Cookie", "labbonet_session=abc123")
	h.Set("X-Labbonet", "1")
	h.Set("Content-Type", "application/json")

	out := RedactHeaders(h)

	if got := out.Get("Authorization"); got != "[redacted]" {
		t.Fatalf("Authorization = %q, want [redacted]", got)
	}
	if got := out.Get("Cookie"); got != "[redacted]" {
		t.Fatalf("Cookie = %q, want [redacted]", got)
	}
	if got := out.Get("X-Labbonet"); got != "1" {
		t.Fatalf("X-Labbonet = %q, want unchanged", got)
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want unchanged", got)
	}

	// The original header is untouched.
	if h.Get("Authorization") != "Bearer sk-secret" {
		t.Fatal("RedactHeaders mutated the input header")
	}
}
