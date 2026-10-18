package openai

import (
	"context"
	"errors"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status int
		msg    string
		want   providers.ErrorKind
	}{
		{401, "", providers.ErrAuth},
		{403, "", providers.ErrAuth},
		{404, "", providers.ErrModelNotFound},
		{409, "", providers.ErrBusy},
		{503, "No models loaded. Please load a model.", providers.ErrNoModelLoaded},
		{503, "server busy", providers.ErrBusy},
		{400, "context window exceeded", providers.ErrContextOverflow},
		{400, "no model loaded", providers.ErrNoModelLoaded},
		{400, "something else entirely", providers.ErrUpstreamError},
		{500, "", providers.ErrUpstreamError},
	}
	for _, c := range cases {
		if got := classifyStatus(c.status, c.msg); got != c.want {
			t.Errorf("classifyStatus(%d, %q) = %q, want %q", c.status, c.msg, got, c.want)
		}
	}
}

func TestExtractErrorMessage(t *testing.T) {
	if got := extractErrorMessage([]byte(`{"error":{"type":"forbidden","message":"nope"}}`)); got != "nope" {
		t.Fatalf("got %q, want %q", got, "nope")
	}
	if got := extractErrorMessage([]byte("plain text error")); got != "plain text error" {
		t.Fatalf("got %q", got)
	}
}

func TestMapRequestError_Cancelled(t *testing.T) {
	err := mapRequestError(context.Canceled)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrCancelled {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestMapRequestError_Timeout(t *testing.T) {
	err := mapRequestError(context.DeadlineExceeded)
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrTimeout {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestMapRequestError_Unreachable(t *testing.T) {
	err := mapRequestError(errors.New("connection refused"))
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrUnreachable {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}
