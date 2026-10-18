package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

// newSSEScanner returns a line scanner over an SSE response body, with
// a generous buffer for long chunks (a full assistant turn can arrive
// in one chunk on some servers).
func newSSEScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	return scanner
}

// sseLine is one classified "data:" line.
type sseLine struct {
	data string
	done bool
}

// parseSSELine classifies one line of an SSE stream (PROVIDER-SPEC.md
// section 1.1): ok is false for a blank line, a keep-alive comment
// (Strata sends ": keep-alive" during prompt reads), or any field other
// than "data:" (event:, id:, retry:, all ignored). done is true only
// for the terminal "data: [DONE]" line.
func parseSSELine(line string) (sseLine, bool) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return sseLine{}, false
	}
	if strings.HasPrefix(line, ":") {
		return sseLine{}, false
	}
	data, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return sseLine{}, false
	}
	data = strings.TrimSpace(data)
	if data == "[DONE]" {
		return sseLine{done: true}, true
	}
	return sseLine{data: data}, true
}

// streamChunk is the OpenAI-compatible SSE chunk shape, extended with
// the Strata timings object (Strata_Protocol.md section 5.3) and the
// LM Studio v0 chat stats shape (PROVIDER-SPEC.md section 5.2,
// untested as of Phase 1). Unknown fields are ignored.
type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          *string `json:"content"`
			ReasoningContent *string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Timings *struct {
		PromptMs           float64 `json:"prompt_ms"`
		PredictedPerSecond float64 `json:"predicted_per_second"`
	} `json:"timings"`
	Stats *struct {
		TokensPerSecond  float64 `json:"tokens_per_second"`
		TimeToFirstToken float64 `json:"time_to_first_token"`
	} `json:"stats"`
}

// normalizeChunk turns one decoded SSE data chunk into zero or more
// normalized events. A null delta.content with finish_reason "length"
// is a truncation, not an error (Strata_Protocol.md section 5.4), so it
// simply produces no delta event.
func normalizeChunk(raw string) ([]providers.Event, error) {
	var chunk streamChunk
	if err := json.Unmarshal([]byte(raw), &chunk); err != nil {
		return nil, err
	}

	var events []providers.Event
	if len(chunk.Choices) > 0 {
		c := chunk.Choices[0]
		if c.Delta.Content != nil && *c.Delta.Content != "" {
			events = append(events, providers.Event{Kind: providers.EventDelta, Content: *c.Delta.Content})
		}
		if c.Delta.ReasoningContent != nil && *c.Delta.ReasoningContent != "" {
			events = append(events, providers.Event{Kind: providers.EventDelta, Reasoning: *c.Delta.ReasoningContent})
		}
		if c.FinishReason != nil && *c.FinishReason != "" {
			events = append(events, providers.Event{
				Kind:          providers.EventDone,
				FinishReason:  *c.FinishReason,
				ModelReported: chunk.Model,
			})
		}
	}

	if chunk.Usage != nil {
		cached := 0
		if chunk.Usage.PromptTokensDetails != nil {
			cached = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		events = append(events, providers.Event{
			Kind:             providers.EventUsage,
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			CachedTokens:     cached,
		})
	}

	switch {
	case chunk.Timings != nil:
		events = append(events, providers.Event{
			Kind:            providers.EventTimings,
			TTFTMillis:      chunk.Timings.PromptMs,
			TokensPerSecond: chunk.Timings.PredictedPerSecond,
		})
	case chunk.Stats != nil:
		events = append(events, providers.Event{
			Kind:            providers.EventTimings,
			TTFTMillis:      chunk.Stats.TimeToFirstToken * 1000,
			TokensPerSecond: chunk.Stats.TokensPerSecond,
		})
	}

	return events, nil
}
