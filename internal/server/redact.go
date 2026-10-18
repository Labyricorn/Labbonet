package server

import (
	"net/http"
	"strings"
)

// sensitiveHeaderNames are header names whose values must never reach a
// log, diagnostics response or backup (SECURITY-MODEL.md section 7:
// "Logs contain request metadata only: no message content and no
// Authorization header values.").
var sensitiveHeaderNames = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"set-cookie":    true,
}

// RedactHeaders returns a copy of h with sensitive header values
// replaced by "[redacted]". Any current or future code that wants to
// show request headers (debug logging, diagnostics) must go through
// this first.
func RedactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, v := range h {
		if sensitiveHeaderNames[strings.ToLower(k)] {
			out[k] = []string{"[redacted]"}
			continue
		}
		out[k] = append([]string(nil), v...)
	}
	return out
}
