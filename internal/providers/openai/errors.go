package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

type errorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// extractErrorMessage reads the "error" shape observed from Strata and
// LM Studio ({"error": {"type", "message"}}, Strata_Protocol.md section
// 7); falling back to the raw body if it doesn't parse.
func extractErrorMessage(body []byte) string {
	var eb errorBody
	if err := json.Unmarshal(body, &eb); err == nil && eb.Error.Message != "" {
		return eb.Error.Message
	}
	return strings.TrimSpace(string(body))
}

// mapHTTPError classifies an HTTP error response into the normalized
// taxonomy (PROVIDER-SPEC.md section 1.3).
func mapHTTPError(status int, body []byte) error {
	msg := extractErrorMessage(body)
	return &providers.Error{Kind: classifyStatus(status, msg), Message: msg, Err: fmt.Errorf("http %d", status)}
}

func classifyStatus(status int, msg string) providers.ErrorKind {
	lower := strings.ToLower(msg)
	switch status {
	case 401, 403:
		return providers.ErrAuth
	case 404:
		return providers.ErrModelNotFound
	case 409:
		return providers.ErrBusy
	case 503:
		if strings.Contains(lower, "no model") || strings.Contains(lower, "not loaded") {
			return providers.ErrNoModelLoaded
		}
		return providers.ErrBusy
	case 400:
		if strings.Contains(lower, "context") {
			return providers.ErrContextOverflow
		}
		if strings.Contains(lower, "no model") || strings.Contains(lower, "not loaded") {
			return providers.ErrNoModelLoaded
		}
		return providers.ErrUpstreamError
	}
	return providers.ErrUpstreamError
}

// mapRequestError classifies a transport-level failure (connection
// refused, dial guard refusal, DNS failure, timeout) into the
// normalized taxonomy. It never sees HTTP status codes, since the
// request did not get a response.
func mapRequestError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &providers.Error{Kind: providers.ErrTimeout, Message: "request timed out", Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &providers.Error{Kind: providers.ErrCancelled, Message: "request cancelled", Err: err}
	}
	if strings.Contains(err.Error(), "timeout") {
		return &providers.Error{Kind: providers.ErrTimeout, Message: err.Error(), Err: err}
	}
	return &providers.Error{Kind: providers.ErrUnreachable, Message: err.Error(), Err: err}
}
