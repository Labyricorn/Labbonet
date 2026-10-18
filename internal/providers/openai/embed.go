package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

// Request bounds: a bad caller cannot turn one call into an unbounded upload.
const (
	maxEmbedInputs     = 64
	maxEmbedInputBytes = 16 << 10
	maxEmbedDim        = 8192
)

type embedRequestBody struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

type embedResponseBody struct {
	Model string `json:"model"`
	Data  []struct {
		Index     *int      `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

// Embed implements providers.Embedder with the OpenAI-compatible request shape
// ({"model","input":[...],"encoding_format":"float"} -> {"data":[{"index","embedding"}]}).
// Wire compatibility is checked against a scripted server in tests; a live LM Studio
// response is not yet verified (PROVIDER-SPEC.md section 5.3).
func (a *Adapter) Embed(ctx context.Context, req providers.EmbedRequest) (providers.EmbedResult, error) {
	if req.Model == "" || len(req.Inputs) == 0 || len(req.Inputs) > maxEmbedInputs {
		return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "invalid embedding request"}
	}
	for _, in := range req.Inputs {
		if in == "" || len(in) > maxEmbedInputBytes {
			return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "embedding input is empty or too large"}
		}
	}
	raw, err := json.Marshal(embedRequestBody{Model: req.Model, Input: req.Inputs, EncodingFormat: "float"})
	if err != nil {
		return providers.EmbedResult{}, err
	}
	resp, data, err := a.egress.DoLimited(ctx, a.target, http.MethodPost, "/v1/embeddings", bytes.NewReader(raw))
	if err != nil {
		return providers.EmbedResult{}, mapRequestError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return providers.EmbedResult{}, mapHTTPError(resp.StatusCode, data)
	}
	var body embedResponseBody
	if err := json.Unmarshal(data, &body); err != nil {
		return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "malformed embedding response", Err: err}
	}
	if len(body.Data) != len(req.Inputs) {
		return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: fmt.Sprintf("expected %d embeddings, got %d", len(req.Inputs), len(body.Data))}
	}
	sort.SliceStable(body.Data, func(i, j int) bool {
		if body.Data[i].Index == nil || body.Data[j].Index == nil {
			return false
		}
		return *body.Data[i].Index < *body.Data[j].Index
	})
	out := providers.EmbedResult{ModelReported: body.Model, PromptTokens: body.Usage.PromptTokens}
	dim := 0
	for i, d := range body.Data {
		if d.Index != nil && *d.Index != i {
			return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "embedding indexes are not 0..n-1"}
		}
		if len(d.Embedding) == 0 || len(d.Embedding) > maxEmbedDim || (dim != 0 && len(d.Embedding) != dim) {
			return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "embedding has an invalid or inconsistent size"}
		}
		dim = len(d.Embedding)
		v := make([]float32, dim)
		for j, f := range d.Embedding {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return providers.EmbedResult{}, &providers.Error{Kind: providers.ErrProtocolError, Message: "embedding contains a non-finite value"}
			}
			v[j] = float32(f)
		}
		out.Vectors = append(out.Vectors, v)
	}
	return out, nil
}

var _ providers.Embedder = (*Adapter)(nil)
