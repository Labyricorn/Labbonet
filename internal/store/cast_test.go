package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Labyricorn/Labbonet/internal/cast"
	"path/filepath"
	"testing"
)

func TestCastDurabilityDefaultsConflictsAndHistoricalIdentity(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "cast.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	save := func(kind, name string) *cast.Card {
		t.Helper()
		c, err := st.SaveCard(ctx, cast.Card{Kind: kind, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b, p, q := save("character", "A"), save("character", "B"), save("persona", "P"), save("persona", "Q")
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := st.ListCards(ctx)
	if err != nil || len(persisted) != 4 {
		t.Fatal("two characters and two personas must survive restart", persisted, err)
	}
	defaults, _ := st.GetCast(ctx, 0)
	defaults.CharacterID = &a.ID
	defaults.PersonaID = &p.ID
	defaults, err = st.SaveCast(ctx, 0, defaults)
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(ctx, "frozen", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	sel, _ := st.GetCast(ctx, conv.ID)
	defaults.CharacterID = &b.ID
	defaults.PersonaID = &q.ID
	if _, err = st.SaveCast(ctx, 0, defaults); err != nil {
		t.Fatal(err)
	}
	frozen, _, _, err := st.ReadCast(ctx, conv.ID)
	if err != nil || *frozen.CharacterID != a.ID || *frozen.PersonaID != p.ID {
		t.Fatal(frozen, err)
	}
	wrong := sel
	wrong.CharacterID = &p.ID
	if _, err = st.SaveCast(ctx, conv.ID, wrong); !errors.Is(err, ErrInvalidCast) {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cast.Speaker(a))
	if err = st.AddGreeting(ctx, conv.ID, sel.Revision, "hello", string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = st.AddGreeting(ctx, conv.ID, sel.Revision, "duplicate", string(raw)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = st.SaveCast(ctx, conv.ID, sel); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	old := *a
	a.Name = "Renamed"
	a, err = st.SaveCard(ctx, *a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveCard(ctx, old); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = st.DeleteCard(ctx, a.ID, old.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = st.DeleteCard(ctx, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cards, err := st.ListCards(ctx)
	if err != nil || len(cards) != 3 {
		t.Fatal(cards, err)
	}
	msgs, err := st.ListMessages(ctx, conv.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatal(msgs, err)
	}
	var identity cast.Identity
	if err = json.Unmarshal([]byte(msgs[0].SpeakerJSON), &identity); err != nil || identity.Name != "A" {
		t.Fatal(identity, err)
	}
	frozen, _ = st.GetCast(ctx, conv.ID)
	if frozen.CharacterID != nil || *frozen.PersonaID != p.ID {
		t.Fatal(frozen)
	}
}

func TestReorderIsAtomicAndStaleListsConflict(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	a, _ := st.SaveCard(ctx, cast.Card{Kind: "character", Name: "A"})
	b, _ := st.SaveCard(ctx, cast.Card{Kind: "character", Name: "B"})
	if err = st.ReorderCards(ctx, "character", []CardOrder{{b.ID, b.Revision}, {a.ID, 99}}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	got, _ := st.GetCard(ctx, b.ID)
	if got.Revision != b.Revision {
		t.Fatal("partial reorder committed")
	}
	if err = st.ReorderCards(ctx, "character", []CardOrder{{b.ID, b.Revision}, {a.ID, a.Revision}}); err != nil {
		t.Fatal(err)
	}
	cards, _ := st.ListCards(ctx)
	if cards[0].ID != b.ID || cards[1].ID != a.ID {
		t.Fatal(cards)
	}
	if err = st.ReorderCards(ctx, "character", []CardOrder{{b.ID, b.Revision}, {a.ID, a.Revision}}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestTurnRejectsChangedHistoryAndConcurrentGeneration(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ep, _ := st.CreateEndpoint(ctx, "test", "openai", "http://127.0.0.1:1", "local", 1)
	conv, _ := st.CreateConversation(ctx, "test", nil, nil, "", "{}")
	sel, _ := st.GetCast(ctx, conv.ID)
	if _, _, err = st.StartTurn(ctx, conv.ID, ep.ID, "m", "hi", "{}", "{}", "{}", 0, sel.Revision); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 2} {
		if _, _, err = st.StartTurn(ctx, conv.ID, ep.ID, "m", "again", "{}", "{}", "{}", n, sel.Revision); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	msgs, _ := st.ListMessages(ctx, conv.ID)
	if len(msgs) != 2 {
		t.Fatal("rejected turn persisted", msgs)
	}
	if err = st.AddGreeting(ctx, conv.ID, sel.Revision, "oops", "{}"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
