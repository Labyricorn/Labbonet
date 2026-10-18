package prompt

import (
	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/providers"
	"strings"
	"testing"
	"time"
)

func TestAssembleResolvedPromptAndWholeTurnBudget(t *testing.T) {
	temp := 0.4
	c := &cast.Card{Name: "Archivist", Description: "Hello {{user}}, {{custom}}", Macros: map[string]string{"custom": "{{date}}"}, Carousel: []string{"first", "second"}, ReplyLimit: 32, Temperature: &temp}
	p := &cast.Card{Name: "Reader", Description: "curious"}
	history := []providers.Message{{Role: "user", Content: strings.Repeat("x", 200)}, {Role: "assistant", Content: strings.Repeat("y", 200)}, {Role: "user", Content: "recent"}, {Role: "assistant", Content: "answer"}}
	got, err := Assemble(Input{Character: c, Persona: p, History: history, User: "now", Turn: 1, ContextLimit: 300, Now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if got.OmittedMessages != 2 || got.CarouselIndex != 1 || got.MaxTokens != 32 || *got.Temperature != temp {
		t.Fatalf("bad budget/params: %+v", got)
	}
	if !strings.Contains(got.Messages[0].Content, "Hello Reader, 2026-10-18") || !strings.Contains(got.Messages[0].Content, "second") {
		t.Fatalf("unresolved system: %s", got.Messages[0].Content)
	}
	if got.Messages[1].Content != "recent" || got.Messages[len(got.Messages)-1].Content != "now" {
		t.Fatal(got.Messages)
	}
}

func TestExpandRejectsCyclesAndExpansionBombs(t *testing.T) {
	for _, v := range []map[string]string{{"a": "{{a}}"}, {"a": "{{b}}", "b": "{{a}}"}, {"a": strings.Repeat("x", 262145)}} {
		if _, err := Expand("{{a}}", v); err == nil {
			t.Fatal("accepted invalid expansion")
		}
	}
	got, err := Expand("<script>alert(1)</script>{{unknown}}", nil)
	if err != nil || got != "<script>alert(1)</script>{{unknown}}" {
		t.Fatal(got, err)
	}
}

func TestAssembleRejectsOversizeWithoutTruncatingCurrentTurn(t *testing.T) {
	if _, err := Assemble(Input{User: strings.Repeat("x", 300), ContextLimit: 256, MaxTokens: 10}); err == nil {
		t.Fatal("accepted oversized prompt")
	}
}

func TestGreetingUsesPersonaAndCharacterMacros(t *testing.T) {
	c := &cast.Card{Name: "C", Greetings: []string{"{{char}} greets {{user}} {{a}}"}, Macros: map[string]string{"a": "{{b}}"}}
	p := &cast.Card{Name: "P", Macros: map[string]string{"b": "welcome"}}
	got, err := Greeting(c, p, 0, time.Now())
	if err != nil || got != "C greets P welcome" {
		t.Fatal(got, err)
	}
	if _, err := Greeting(c, p, 1, time.Now()); err == nil {
		t.Fatal("accepted invalid variant")
	}
}

func TestPersonaCadenceMatchesPinnedReference(t *testing.T) {
	for _, freq := range []int{0, 1, 5} {
		for turn := 0; turn < 6; turn++ {
			got, err := Assemble(Input{Persona: &cast.Card{Name: "Reader", Description: "curious", PersonaFrequency: freq}, User: "hi", Turn: turn})
			if err != nil {
				t.Fatal(err)
			}
			want := freq > 0 && (turn == 0 || (turn+1)%freq == 0)
			found := len(got.Messages) > 1 && got.Messages[len(got.Messages)-2].Role == "system" && strings.Contains(got.Messages[len(got.Messages)-2].Content, "curious")
			if found != want {
				t.Fatalf("freq=%d turn=%d got persona=%v want %v", freq, turn, found, want)
			}
		}
	}
}
