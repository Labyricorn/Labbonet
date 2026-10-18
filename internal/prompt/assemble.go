// Package prompt assembles reproducible, bounded requests without executing template text.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design reference: GobboNet, Copyright (C) 2026 Elodine / GoblinCorps,
// pin 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (08-rag.js persona cadence/tail;
// 07-prompt.js/17-personas.js template concepts). No upstream source copied.
// Exact attribution boundaries: docs/REUSE-LOG.md.
package prompt

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/memory"
	"github.com/Labyricorn/Labbonet/internal/providers"
)

type Input struct {
	Character    *cast.Card
	Persona      *cast.Card
	System       string
	History      []providers.Message
	User         string
	Turn         int
	ContextLimit int
	MaxTokens    int
	Temperature  *float64
	TopP         *float64
	Now          time.Time
	// Summary is the running summary of messages the caller already removed from
	// History (ArchivedMessages of them). It joins the system message and is dropped,
	// with a note, if it cannot fit beside the system text and the current turn.
	Summary          string
	SummarySeq       int64
	ArchivedMessages int
	// Retrieve, when set, is called once with the headroom (in memory.Cost units)
	// retrieval may use and returns storybook text to inject before the current turn.
	// The caller guarantees the returned cost never exceeds the headroom it was given.
	Retrieve func(headroom int) []memory.Injection
}
type Snapshot struct {
	Version         int                 `json:"version"`
	Character       *cast.Card          `json:"character,omitempty"`
	Persona         *cast.Card          `json:"persona,omitempty"`
	Messages        []providers.Message `json:"messages"`
	MaxTokens       int                 `json:"max_tokens"`
	Temperature     *float64            `json:"temperature,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	ContextLimit    int                 `json:"context_limit"`
	EstimatedTokens int                 `json:"estimated_tokens"`
	OmittedMessages int                 `json:"omitted_messages"`
	CarouselIndex   int                 `json:"carousel_index"`
	AssembledAt     time.Time           `json:"assembled_at"`
	// Memory provenance: which summary revision (0 = none), how many messages it
	// replaced, which storybook entries were injected and what was left out.
	SummarySeq       int64    `json:"summary_seq,omitempty"`
	ArchivedMessages int      `json:"archived_messages,omitempty"`
	SummaryOmitted   bool     `json:"summary_omitted,omitempty"`
	Retrieved        []string `json:"retrieved,omitempty"`
	RetrievedCost    int      `json:"retrieved_cost,omitempty"`
}

var macro = regexp.MustCompile(`\{\{([a-zA-Z][a-zA-Z0-9_]*)\}\}`)

// Expand is bounded and rejects cycles. Unknown macros remain visible, inert text.
func Expand(text string, values map[string]string) (string, error) {
	for depth := 0; depth < 16; depth++ {
		if len(text) > 262144 {
			return "", errors.New("expanded prompt exceeds 256 KiB")
		}
		changed := false
		var b strings.Builder
		cursor := 0
		for _, loc := range macro.FindAllStringIndex(text, -1) {
			v, ok := values[text[loc[0]+2:loc[1]-2]]
			if !ok {
				continue
			}
			changed = true
			if b.Len()+loc[0]-cursor+len(v) > 262144 {
				return "", errors.New("expanded prompt exceeds 256 KiB")
			}
			b.WriteString(text[cursor:loc[0]])
			b.WriteString(v)
			cursor = loc[1]
		}
		if !changed {
			return text, nil
		}
		if b.Len()+len(text)-cursor > 262144 {
			return "", errors.New("expanded prompt exceeds 256 KiB")
		}
		b.WriteString(text[cursor:])
		next := b.String()
		if next == text {
			return "", errors.New("macro cycle")
		}
		text = next
	}
	return "", errors.New("macro expansion exceeds 16 levels (possible cycle)")
}

func Assemble(in Input) (Snapshot, error) {
	out := Snapshot{Version: 1, Character: snapshotCard(in.Character), Persona: snapshotCard(in.Persona), MaxTokens: in.MaxTokens, Temperature: in.Temperature, TopP: in.TopP, ContextLimit: in.ContextLimit, CarouselIndex: -1, AssembledAt: in.Now}
	if out.ContextLimit <= 0 {
		out.ContextLimit = 8192
	}
	values := map[string]string{"char": "Assistant", "user": "You", "date": in.Now.Format("2006-01-02"), "time": in.Now.Format("15:04"), "carousel": ""}
	if in.Persona != nil {
		values["user"] = in.Persona.Name
		for k, v := range in.Persona.Macros {
			values[k] = v
		}
	}
	if c := in.Character; c != nil {
		values["char"] = c.Name
		for k, v := range c.Macros {
			values[k] = v
		}
		if c.ContextLimit > 0 && c.ContextLimit < out.ContextLimit {
			out.ContextLimit = c.ContextLimit
		}
		if out.MaxTokens == 0 {
			out.MaxTokens = c.ReplyLimit
		}
		if out.Temperature == nil {
			out.Temperature = c.Temperature
		}
		if out.TopP == nil {
			out.TopP = c.TopP
		}
		if len(c.Carousel) > 0 {
			idx := in.Turn % len(c.Carousel)
			if c.CarouselMode == "random" {
				idx = rand.IntN(len(c.Carousel))
			}
			out.CarouselIndex = idx
			values["carousel"] = c.Carousel[idx]
		}
	}
	if out.MaxTokens == 0 {
		out.MaxTokens = 256
	}
	if out.MaxTokens < 1 || out.MaxTokens > 32768 || out.MaxTokens >= out.ContextLimit {
		return out, errors.New("reply limit must fit inside the context window")
	}
	if out.Temperature != nil && (math.IsNaN(*out.Temperature) || math.IsInf(*out.Temperature, 0) || *out.Temperature < 0 || *out.Temperature > 2) {
		return out, errors.New("invalid temperature")
	}
	if out.TopP != nil && (math.IsNaN(*out.TopP) || math.IsInf(*out.TopP, 0) || *out.TopP <= 0 || *out.TopP > 1) {
		return out, errors.New("invalid top_p")
	}
	parts := []string{in.System}
	if c := in.Character; c != nil {
		parts = append(parts, c.SystemPrompt, "Character: "+c.Name, c.Description, c.Personality, c.Style, c.Lore, "{{carousel}}")
	}
	// Pinned GobboNet 08-rag.js: description at tail on user message 1,
	// then multiples of frequency. Zero disables description, retaining identity.
	personaText := ""
	if p := in.Persona; p != nil && p.PersonaFrequency > 0 && (in.Turn == 0 || (in.Turn+1)%p.PersonaFrequency == 0) {
		var err error
		personaText, err = Expand("[Persona: "+p.Name+"]\n"+p.Description, values)
		if err != nil {
			return out, err
		}
	}
	system, err := Expand(strings.Join(parts, "\n\n"), values)
	if err != nil {
		return out, err
	}
	system = strings.TrimSpace(system)
	// UTF-8 byte count + framing is deliberately conservative in the absence of a
	// verified tokenizer. Never silently truncate the system or current user turn.
	used := len(in.User) + 16 + out.MaxTokens
	if personaText != "" {
		used += len(personaText) + 16
	}
	if system != "" {
		used += len(system) + 16
	}
	if used > out.ContextLimit {
		return out, fmt.Errorf("system and current turn exceed context budget (%d > %d); shorten them or raise context limit", used, out.ContextLimit)
	}
	out.ArchivedMessages = in.ArchivedMessages
	if summary := strings.TrimSpace(in.Summary); summary != "" {
		block := "[Summary of earlier conversation]\n" + summary
		if cost := len(block) + 16; used+cost > out.ContextLimit {
			out.SummaryOmitted = true
		} else {
			used += cost
			out.SummarySeq = in.SummarySeq
			system = strings.TrimSpace(system + "\n\n" + block)
		}
	}
	// Retrieval gets at most a quarter of the context and never what the fixed parts
	// above already use; unused headroom goes back to the conversation history.
	loreText := ""
	if in.Retrieve != nil {
		headroom := out.ContextLimit - used
		if limit := out.ContextLimit / 4; headroom > limit {
			headroom = limit
		}
		if headroom > 0 {
			injected := in.Retrieve(headroom)
			for len(injected) > 0 {
				parts := make([]string, len(injected))
				for i, inj := range injected {
					parts[i] = inj.Text
				}
				text := "[Relevant lore]\n" + strings.Join(parts, "\n\n")
				if len(text)+16 <= headroom {
					loreText = text
					break
				}
				injected = injected[:len(injected)-1]
			}
			for _, inj := range injected {
				out.Retrieved = append(out.Retrieved, inj.EntityID)
			}
			if loreText != "" {
				out.RetrievedCost = len(loreText) + 16
				used += out.RetrievedCost
			}
		}
	}
	start := len(in.History)
	for start > 0 {
		// Keep whole user/assistant turns, avoiding orphan assistants at the boundary.
		candidate := start - 1
		for candidate > 0 && in.History[candidate].Role != "user" {
			candidate--
		}
		cost := 0
		for _, m := range in.History[candidate:start] {
			cost += len(m.Content) + 16
		}
		if used+cost > out.ContextLimit {
			break
		}
		used += cost
		start = candidate
	}
	out.OmittedMessages = start
	out.Messages = []providers.Message{}
	if system != "" {
		out.Messages = append(out.Messages, providers.Message{Role: "system", Content: system})
	}
	out.Messages = append(out.Messages, in.History[start:]...)
	if loreText != "" {
		out.Messages = append(out.Messages, providers.Message{Role: "system", Content: loreText})
	}
	if personaText != "" {
		out.Messages = append(out.Messages, providers.Message{Role: "system", Content: personaText})
	}
	out.Messages = append(out.Messages, providers.Message{Role: "user", Content: in.User})
	out.EstimatedTokens = used - out.MaxTokens
	return out, nil
}

func Greeting(c *cast.Card, p *cast.Card, index int, now time.Time) (string, error) {
	if c == nil || len(c.Greetings) == 0 {
		return "", nil
	}
	if index < 0 || index >= len(c.Greetings) {
		return "", errors.New("greeting variant out of range")
	}
	values := map[string]string{"carousel": ""}
	if p != nil {
		for k, v := range p.Macros {
			values[k] = v
		}
	}
	for k, v := range c.Macros {
		values[k] = v
	}
	values["char"] = c.Name
	values["user"] = "You"
	if p != nil {
		values["user"] = p.Name
	}
	values["date"] = now.Format("2006-01-02")
	values["time"] = now.Format("15:04")
	return Expand(c.Greetings[index], values)
}

// Prompt provenance keeps revisions and resolved text, not duplicated image assets
// or unused greeting variants on every generation. Message portraits remain in
// immutable speaker snapshots for historical display.
func snapshotCard(c *cast.Card) *cast.Card {
	if c == nil {
		return nil
	}
	copy := *c
	copy.Portrait = ""
	copy.Background = ""
	copy.Greetings = nil
	return &copy
}
