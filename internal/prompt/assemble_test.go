package prompt

import (
	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/memory"
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

func TestAssembleSummaryJoinsSystemAndIsDroppedWhenItCannotFit(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	c := &cast.Card{Name: "C", Description: "desc"}
	got, err := Assemble(Input{Character: c, User: "now", ContextLimit: 2000, MaxTokens: 50, Summary: "They met a dragon.", SummarySeq: 3, ArchivedMessages: 12, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	sys := got.Messages[0].Content
	if got.Messages[0].Role != "system" || !strings.Contains(sys, "[Summary of earlier conversation]\nThey met a dragon.") || got.SummarySeq != 3 || got.ArchivedMessages != 12 || got.SummaryOmitted {
		t.Fatalf("summary not applied: %+v / %s", got, sys)
	}
	if n := strings.Count(strings.Join(msgRoles(got.Messages), ","), "system"); n != 1 {
		t.Fatalf("summary must not add a second system message: %v", msgRoles(got.Messages))
	}
	// Too small to hold the summary beside the system text and the turn: dropped, not an error.
	got, err = Assemble(Input{Character: c, User: "now", ContextLimit: 256, MaxTokens: 50, Summary: strings.Repeat("s", 400), SummarySeq: 3, ArchivedMessages: 12, Now: now})
	if err != nil || !got.SummaryOmitted || got.SummarySeq != 0 || strings.Contains(got.Messages[0].Content, "Summary") {
		t.Fatalf("oversized summary kept: %+v err=%v", got, err)
	}
}

func msgRoles(ms []providers.Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Role)
	}
	return out
}

func TestAssembleRetrievalIsBoundedAndSitsBeforeTheTurn(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	var offered int
	retrieve := func(headroom int) []memory.Injection {
		offered = headroom
		return []memory.Injection{{EntityID: "a", Text: strings.Repeat("a", 60)}, {EntityID: "b", Text: strings.Repeat("b", 400)}}
	}
	history := []providers.Message{{Role: "user", Content: "old"}, {Role: "assistant", Content: "older"}}
	got, err := Assemble(Input{History: history, User: "now", ContextLimit: 1000, MaxTokens: 50, Retrieve: retrieve, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if offered != 250 {
		t.Fatalf("headroom offered=%d, want a quarter of the context", offered)
	}
	// "b" does not fit in the offered headroom and is dropped; "a" is injected as one
	// system message directly before the user turn.
	if len(got.Retrieved) != 1 || got.Retrieved[0] != "a" || got.RetrievedCost > 250 {
		t.Fatalf("retrieved=%v cost=%d", got.Retrieved, got.RetrievedCost)
	}
	n := len(got.Messages)
	if got.Messages[n-1].Content != "now" || got.Messages[n-2].Role != "system" || !strings.HasPrefix(got.Messages[n-2].Content, "[Relevant lore]\n") {
		t.Fatalf("order: %+v", got.Messages)
	}
	// Retrieval spends headroom before history is fitted, so history yields, not the turn.
	big := []providers.Message{{Role: "user", Content: strings.Repeat("h", 500)}, {Role: "assistant", Content: strings.Repeat("h", 300)}}
	got, err = Assemble(Input{History: big, User: "now", ContextLimit: 1000, MaxTokens: 50, Retrieve: func(int) []memory.Injection {
		return []memory.Injection{{EntityID: "a", Text: strings.Repeat("a", 200)}}
	}, Now: now})
	if err != nil || got.OmittedMessages != 2 || len(got.Retrieved) != 1 || got.Messages[len(got.Messages)-1].Content != "now" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	// No headroom: retrieval is not even called.
	called := false
	if _, err = Assemble(Input{User: strings.Repeat("u", 934), ContextLimit: 1000, MaxTokens: 50, Retrieve: func(int) []memory.Injection { called = true; return nil }, Now: now}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("retrieval called without headroom")
	}
}
