package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Labyricorn/Labbonet/internal/providers"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatRequestBody is the request mapping in PROVIDER-SPEC.md section
// 1.2. ReasoningEffort is only ever set for Strata; Labbonet never
// sends "ttl" (would change LM Studio's model lifetime) or
// "strata_mcp" (Strata's own MCP tools) at all, by never having a field
// for them here.
type chatRequestBody struct {
	Model           string         `json:"model"`
	Messages        []chatMessage  `json:"messages"`
	Stream          bool           `json:"stream"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	MaxTokens       int            `json:"max_tokens,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	TopP            *float64       `json:"top_p,omitempty"`
	Stop            []string       `json:"stop,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

// buildChatBody maps a normalized ChatRequest to the wire body,
// applying the per-profile extras allowlist (PROVIDER-SPEC.md section
// 1.2): only Strata gets reasoning_effort.
func (a *Adapter) buildChatBody(req providers.ChatRequest) chatRequestBody {
	msgs := make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	body := chatRequestBody{
		Model:         req.Model,
		Messages:      msgs,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		Stop:          req.Stop,
	}
	if a.profile == ProfileStrata {
		body.ReasoningEffort = req.ReasoningEffort
	}
	return body
}

// StreamChat issues a streaming chat completion and relays normalized
// events to emit. Cancelling ctx aborts the upstream request; there is
// no separate cancel call (PROVIDER-SPEC.md section 1).
func (a *Adapter) StreamChat(ctx context.Context, req providers.ChatRequest, emit func(providers.Event) error) (providers.Result, error) {
	if a.profile == "" {
		if _, err := a.Detect(ctx); err != nil {
			return providers.Result{}, err
		}
	}

	raw, err := json.Marshal(a.buildChatBody(req))
	if err != nil {
		return providers.Result{}, fmt.Errorf("openai: marshal chat request: %w", err)
	}

	started := time.Now()
	resp, err := a.egress.Do(ctx, a.target, http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return providers.Result{}, mapRequestError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return providers.Result{}, mapHTTPError(resp.StatusCode, errBody)
	}

	if err := emit(providers.Event{Kind: providers.EventStart}); err != nil {
		return providers.Result{}, err
	}

	var result providers.Result
	var firstToken time.Time
	var completionTokens int
	var sawTimings bool
	var terminated bool // a [DONE] marker or a finish reason was seen
	scanner := newSSEScanner(resp.Body)
	for scanner.Scan() {
		line, ok := parseSSELine(scanner.Text())
		if !ok {
			continue
		}
		if line.done {
			terminated = true
			break
		}
		events, err := normalizeChunk(line.data)
		if err != nil {
			return result, &providers.Error{Kind: providers.ErrProtocolError, Message: "malformed stream chunk", Err: err}
		}
		for _, ev := range events {
			switch ev.Kind {
			case providers.EventDelta:
				if firstToken.IsZero() && (ev.Content != "" || ev.Reasoning != "") {
					firstToken = time.Now()
				}
			case providers.EventUsage:
				completionTokens = ev.CompletionTokens
			case providers.EventTimings:
				sawTimings = true
			}
			if ev.Kind == providers.EventDone {
				terminated = true
				result.FinishReason = ev.FinishReason
				result.ModelReported = ev.ModelReported
			}
			if err := emit(ev); err != nil {
				return result, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return result, &providers.Error{Kind: providers.ErrCancelled, Message: "generation cancelled", Err: ctx.Err()}
		}
		return result, &providers.Error{Kind: providers.ErrProtocolError, Message: "stream read error", Err: err}
	}
	if !terminated {
		// A clean EOF with neither [DONE] nor a finish reason is a truncated stream, not a
		// completed reply. Text received so far stays with the caller.
		if ctx.Err() != nil {
			return result, &providers.Error{Kind: providers.ErrCancelled, Message: "generation cancelled", Err: ctx.Err()}
		}
		return result, &providers.Error{Kind: providers.ErrProtocolError, Message: "stream ended without a finish marker"}
	}
	// Some OpenAI-compatible streams (including the observed LM Studio
	// /v1 stream) omit stats. Keep provider timings authoritative when
	// supplied; otherwise report client-observed timing, including
	// reasoning as generated output and usage that arrives after done.
	if !sawTimings && !firstToken.IsZero() {
		timing := providers.Event{Kind: providers.EventTimings, TTFTMillis: float64(firstToken.Sub(started)) / float64(time.Millisecond)}
		if seconds := time.Since(firstToken).Seconds(); seconds > 0 && completionTokens > 0 {
			timing.TokensPerSecond = float64(completionTokens) / seconds
		}
		if err := emit(timing); err != nil {
			return result, err
		}
	}
	return result, nil
}
