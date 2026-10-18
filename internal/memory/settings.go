// Package memory holds Labbonet's lore, summary and retrieval logic: the storybook
// parser, the retrieval engine, embedding vectors and the memory settings. It is pure
// (no database, no network) so every decision is testable with fixed inputs.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design reference: GobboNet, Copyright (C) 2026 Elodine / GoblinCorps, pin
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (08-rag.js storybook grammar, weighted tags,
// ranking and warmth; 07-prompt.js summary/archive behavior). No upstream source copied.
// Exact attribution boundaries: docs/REUSE-LOG.md.
package memory

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Settings are the global memory choices. Model choices are (endpoint, model) pairs; the
// embedding pair is never used for chat and the compression pair never for embeddings.
type Settings struct {
	Revision int64 `json:"revision"`

	RetrievalEnabled bool    `json:"retrieval_enabled"`
	RetrieverA       bool    `json:"retriever_a"` // semantic (needs an embedding model)
	RetrieverB       bool    `json:"retriever_b"` // weighted tags
	FireThreshold    float64 `json:"fire_threshold"`
	WarmthTurns      int     `json:"warmth_turns"`
	ExpansionDepth   int     `json:"expansion_depth"` // 0 or 1
	TopKA            int     `json:"top_k_a"`
	SemanticFloor    float64 `json:"semantic_floor"`
	RetrievalBudget  int     `json:"retrieval_budget"` // estimated tokens (see prompt.Cost)
	WindowMessages   int     `json:"window_messages"`

	SummarizeEnabled bool `json:"summarize_enabled"`
	SummaryMaxChars  int  `json:"summary_max_chars"`

	CompressionEndpointID *int64 `json:"compression_endpoint_id"`
	CompressionModel      string `json:"compression_model"`

	EmbeddingEndpointID *int64 `json:"embedding_endpoint_id"`
	EmbeddingModel      string `json:"embedding_model"`
	EmbedDocPrefix      string `json:"embed_doc_prefix"`
	EmbedQueryPrefix    string `json:"embed_query_prefix"`
}

// Defaults: tag retrieval on (a no-op without a storybook), semantic retrieval and
// automatic summaries off until the owner enables them and picks models.
func Defaults() Settings {
	return Settings{
		RetrievalEnabled: true, RetrieverB: true, FireThreshold: 1, WarmthTurns: 3, ExpansionDepth: 1,
		TopKA: 5, SemanticFloor: 0.25, RetrievalBudget: 2400, WindowMessages: 4, SummaryMaxChars: 3000,
	}
}

var errSetting = errors.New("invalid memory setting")

func settingErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errSetting, fmt.Sprintf(format, args...))
}

// IsSettingError reports whether err came from Validate.
func IsSettingError(err error) bool { return errors.Is(err, errSetting) }

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Validate checks ranges only; the store checks that chosen models exist and have the
// right type.
func (s Settings) Validate() error {
	if !finite(s.FireThreshold) || s.FireThreshold < 0.1 || s.FireThreshold > 10 {
		return settingErr("fire threshold must be 0.1 through 10")
	}
	if !finite(s.SemanticFloor) || s.SemanticFloor < 0 || s.SemanticFloor > 1 {
		return settingErr("semantic floor must be 0 through 1")
	}
	if s.WarmthTurns < 0 || s.WarmthTurns > 20 {
		return settingErr("warmth must be 0 through 20 turns")
	}
	if s.ExpansionDepth < 0 || s.ExpansionDepth > 1 {
		return settingErr("expansion depth must be 0 or 1")
	}
	if s.TopKA < 1 || s.TopKA > 20 {
		return settingErr("semantic results must be 1 through 20")
	}
	if s.RetrievalBudget < 0 || s.RetrievalBudget > 100000 {
		return settingErr("retrieval budget must be 0 through 100000")
	}
	if s.WindowMessages < 1 || s.WindowMessages > 20 {
		return settingErr("retrieval window must be 1 through 20 messages")
	}
	if s.SummaryMaxChars < 500 || s.SummaryMaxChars > 20000 {
		return settingErr("summary limit must be 500 through 20000 characters")
	}
	if (s.CompressionEndpointID == nil) != (s.CompressionModel == "") {
		return settingErr("choose both an endpoint and a model for compression, or neither")
	}
	if (s.EmbeddingEndpointID == nil) != (s.EmbeddingModel == "") {
		return settingErr("choose both an endpoint and a model for embeddings, or neither")
	}
	if s.RetrieverA && s.EmbeddingEndpointID == nil {
		return settingErr("semantic retrieval needs an embedding model")
	}
	for _, v := range []string{s.EmbedDocPrefix, s.EmbedQueryPrefix, s.CompressionModel, s.EmbeddingModel} {
		if len(v) > 200 || strings.ContainsAny(v, "\x00\r\n") {
			return settingErr("model names and prefixes must be one line of at most 200 bytes")
		}
	}
	return nil
}
