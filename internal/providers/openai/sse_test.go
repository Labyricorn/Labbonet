package openai

import "testing"

// Built from the observed Strata samples in Strata_Protocol.md section
// 5.3: blank lines and ": keep-alive" comments are ignored, "data:
// [DONE]" is the terminal sentinel, and any other SSE field (event:,
// id:, retry:) is ignored too.
func TestParseSSELine(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantOK   bool
		wantDone bool
		wantData string
	}{
		{"blank line ignored", "", false, false, ""},
		{"keep-alive comment ignored", ": keep-alive", false, false, ""},
		{"done sentinel", "data: [DONE]", true, true, ""},
		{"data chunk", `data: {"a":1}`, true, false, `{"a":1}`},
		{"trailing CR stripped", "data: {\"a\":1}\r", true, false, `{"a":1}`},
		{"non-data field ignored", "event: ping", false, false, ""},
		{"no space after colon", `data:{"a":1}`, true, false, `{"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, ok := parseSSELine(c.line)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && (line.done != c.wantDone || line.data != c.wantData) {
				t.Fatalf("line = %+v, want done=%v data=%q", line, c.wantDone, c.wantData)
			}
		})
	}
}

func TestNormalizeChunk_UnknownFieldsIgnored(t *testing.T) {
	events, err := normalizeChunk(`{"choices":[{"delta":{"content":"hi"}}],"some_future_field":{"x":1}}`)
	if err != nil {
		t.Fatalf("normalizeChunk: %v", err)
	}
	if len(events) != 1 || events[0].Content != "hi" {
		t.Fatalf("events = %+v", events)
	}
}

func TestNormalizeChunk_MalformedJSON(t *testing.T) {
	if _, err := normalizeChunk("{not json"); err == nil {
		t.Fatal("normalizeChunk did not error on malformed JSON")
	}
}

func TestNormalizeChunk_EmptyDeltaProducesNoEvent(t *testing.T) {
	// The first chunk of a Strata stream carries delta.role and empty
	// content; it must not produce a spurious empty delta event.
	events, err := normalizeChunk(`{"choices":[{"delta":{"role":"assistant","content":""},"finish_reason":null}]}`)
	if err != nil {
		t.Fatalf("normalizeChunk: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v, want none", events)
	}
}

func TestNormalizeChunk_LMStudioStatsFallsBackToTimings(t *testing.T) {
	events, err := normalizeChunk(`{"choices":[{"delta":{},"finish_reason":"stop"}],"stats":{"tokens_per_second":12.5,"time_to_first_token":0.4}}`)
	if err != nil {
		t.Fatalf("normalizeChunk: %v", err)
	}
	var sawTimings bool
	for _, ev := range events {
		if ev.Kind == "timings" {
			sawTimings = true
			if ev.TokensPerSecond != 12.5 || ev.TTFTMillis != 400 {
				t.Fatalf("timings event = %+v", ev)
			}
		}
	}
	if !sawTimings {
		t.Fatalf("events = %+v, want a timings event from stats", events)
	}
}
