package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/egress"
	"github.com/Labyricorn/Labbonet/internal/providers"
)

func embedServer(t *testing.T, status int, body string, seen *[]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if seen != nil {
			*seen, _ = io.ReadAll(r.Body)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEmbed_SendsOpenAIShapeAndOrdersByIndex(t *testing.T) {
	var seen []byte
	srv := embedServer(t, 200, `{"model":"nomic","data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}],"usage":{"prompt_tokens":7}}`, &seen)
	a := New(egress.NewClient(), targetFor(t, srv.URL))
	res, err := a.Embed(context.Background(), providers.EmbedRequest{Model: "nomic", Inputs: []string{"first", "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Vectors) != 2 || res.Vectors[0][0] != 1 || res.Vectors[1][1] != 1 || res.ModelReported != "nomic" || res.PromptTokens != 7 {
		t.Fatalf("res=%+v", res)
	}
	var body struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
	}
	if err = json.Unmarshal(seen, &body); err != nil || body.Model != "nomic" || len(body.Input) != 2 || body.Input[0] != "first" || body.EncodingFormat != "float" {
		t.Fatalf("request body %s (%v)", seen, err)
	}
	if strings.Contains(string(seen), "ttl") {
		t.Fatal("request carries a lifetime field")
	}
}

func TestEmbed_RejectsBadRequestsAndResponses(t *testing.T) {
	cases := map[string]string{
		"count mismatch":   `{"data":[{"index":0,"embedding":[1]}]}`,
		"empty vector":     `{"data":[{"index":0,"embedding":[]},{"index":1,"embedding":[1]}]}`,
		"mixed dims":       `{"data":[{"index":0,"embedding":[1,2]},{"index":1,"embedding":[1]}]}`,
		"duplicate index":  `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[1]}]}`,
		"not json":         `<html>`,
		"too many dims":    `{"data":[{"index":0,"embedding":[` + strings.Repeat("1,", maxEmbedDim) + `1]},{"index":1,"embedding":[1]}]}`,
		"non-finite value": `{"data":[{"index":0,"embedding":[1e999]},{"index":1,"embedding":[1]}]}`,
	}
	for name, body := range cases {
		srv := embedServer(t, 200, body, nil)
		a := New(egress.NewClient(), targetFor(t, srv.URL))
		_, err := a.Embed(context.Background(), providers.EmbedRequest{Model: "m", Inputs: []string{"a", "b"}})
		var pe *providers.Error
		if !errors.As(err, &pe) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	a := New(egress.NewClient(), targetFor(t, embedServer(t, 200, `{}`, nil).URL))
	for _, req := range []providers.EmbedRequest{
		{Model: "", Inputs: []string{"a"}},
		{Model: "m"},
		{Model: "m", Inputs: []string{""}},
		{Model: "m", Inputs: []string{strings.Repeat("x", maxEmbedInputBytes+1)}},
		{Model: "m", Inputs: make([]string, maxEmbedInputs+1)},
	} {
		if _, err := a.Embed(context.Background(), req); err == nil {
			t.Errorf("accepted %.30v", req)
		}
	}
}

func TestEmbed_MapsHTTPErrorsAndStaysOnTheAllowedPath(t *testing.T) {
	srv := embedServer(t, 401, `{"error":{"message":"bad key"}}`, nil)
	a := New(egress.NewClient(), targetFor(t, srv.URL))
	_, err := a.Embed(context.Background(), providers.EmbedRequest{Model: "m", Inputs: []string{"a"}})
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Kind != providers.ErrAuth {
		t.Fatalf("err=%v", err)
	}
	if err = egress.CheckPath("/v1/embeddings"); err != nil {
		t.Fatal(err)
	}
	if egress.CheckPath("/v1/embeddings/../models") == nil || egress.CheckPath("/v1/embeddings/x") == nil {
		t.Fatal("embedding path allowlist is not exact")
	}
}
