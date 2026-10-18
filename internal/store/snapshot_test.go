package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/interchange"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// seed creates two characters, two personas, a thread using the cast and a speaker snapshot.
func seed(t *testing.T, st *Store) (convID int64) {
	t.Helper()
	ctx := t.Context()
	a, err := st.SaveCard(ctx, cast.Card{Kind: "character", Name: "A", Greetings: []string{"hi", "yo"}, Lore: "lore"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveCard(ctx, cast.Card{Kind: "character", Name: "B"}); err != nil {
		t.Fatal(err)
	}
	p, err := st.SaveCard(ctx, cast.Card{Kind: "persona", Name: "P", PersonaFrequency: 2})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(ctx, "Thread", nil, nil, "sys", `{"secret":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	sel, err := st.GetCast(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	sel.CharacterID, sel.PersonaID, sel.Greeting = &a.ID, &p.ID, 1
	if _, err = st.SaveCast(ctx, conv.ID, sel); err != nil {
		t.Fatal(err)
	}
	m1, _ := st.CreateMessage(ctx, conv.ID, nil, "user", "hello", "", "complete")
	m2, _ := st.CreateMessage(ctx, conv.ID, &m1.ID, "assistant", "world", "thinking", "cancelled")
	if _, err = st.db.ExecContext(ctx, `UPDATE messages SET speaker_json=? WHERE id=?`, `{"id":9,"revision":3,"name":"A","text_color":"#aabbcc"}`, m2.ID); err != nil {
		t.Fatal(err)
	}
	return conv.ID
}

func counts(t *testing.T, st *Store) [5]int {
	t.Helper()
	var out [5]int
	for i, q := range []string{"cast_cards", "conversations", "messages", "conversation_cast", "endpoints"} {
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + q).Scan(&out[i]); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestSnapshotRoundTripPreservesCastAndIdentityWithoutLocalIDs(t *testing.T) {
	src := openTemp(t)
	seed(t, src)
	snap, err := src.ExportSnapshot(t.Context(), "full", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 3 || len(snap.Conversations) != 1 || snap.Conversations[0].Greeting != 1 {
		t.Fatalf("%+v", snap)
	}
	sp := snap.Conversations[0].Messages[1].Speaker
	if sp == nil || sp.ID != 0 || sp.Revision != 0 || sp.Name != "A" {
		t.Fatalf("speaker %+v", sp)
	}
	dst := openTemp(t)
	res, err := dst.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "add"})
	if err != nil {
		t.Fatal(err)
	}
	if res.CardsAdded != 3 || res.ConversationsAdded != 1 || res.MessagesAdded != 2 {
		t.Fatalf("%+v", res)
	}
	conv, err := dst.ListConversations(t.Context())
	if err != nil || len(conv) != 1 || conv[0].ParamsJSON != "{}" || conv[0].EndpointID != nil {
		t.Fatalf("%v %+v", err, conv)
	}
	sel, ch, pe, err := dst.ReadCast(t.Context(), conv[0].ID)
	if err != nil || ch == nil || ch.Name != "A" || pe == nil || pe.Name != "P" || sel.Greeting != 1 {
		t.Fatalf("%v %+v %+v %+v", err, sel, ch, pe)
	}
	msgs, _ := dst.ListMessages(t.Context(), conv[0].ID)
	if len(msgs) != 2 || msgs[1].Status != "cancelled" || msgs[1].Reasoning != "thinking" || msgs[1].ParentID == nil || *msgs[1].ParentID != msgs[0].ID {
		t.Fatalf("%+v", msgs)
	}
}

func TestSingleThreadExportIncludesOnlyReferencedCast(t *testing.T) {
	st := openTemp(t)
	id := seed(t, st)
	snap, err := st.ExportSnapshot(t.Context(), "conversation", id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Cards) != 2 || len(snap.Conversations) != 1 || snap.Defaults != nil {
		t.Fatalf("%d cards", len(snap.Cards))
	}
	if _, err := st.ExportSnapshot(t.Context(), "conversation", 999); err == nil {
		t.Fatal("missing thread exported")
	}
}

func TestDryRunAndRejectedImportsLeaveDatabaseUnchanged(t *testing.T) {
	st := openTemp(t)
	seed(t, st)
	before := counts(t, st)
	snap, _ := st.ExportSnapshot(t.Context(), "full", 0)
	res, err := st.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "add", DryRun: true})
	if err != nil || res.CardsAdded != 3 || res.CardsRenamed != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	if counts(t, st) != before {
		t.Fatal("dry run changed the database")
	}
	// Invalid documents are rejected before any write.
	bad, _ := st.ExportSnapshot(t.Context(), "full", 0)
	bad.Cards[2].Card.Name = ""
	if _, err = st.ImportSnapshot(t.Context(), bad, ImportOptions{Policy: "add"}); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("%v", err)
	}
	dup, _ := st.ExportSnapshot(t.Context(), "full", 0)
	dup.Cards[1].Key = dup.Cards[0].Key
	if _, err = st.ImportSnapshot(t.Context(), dup, ImportOptions{Policy: "add"}); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("%v", err)
	}
	if _, err = st.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "replace"}); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("%v", err)
	}
	if counts(t, st) != before {
		t.Fatal("rejected import changed the database")
	}
}

func TestFailureAfterPartialWritesRollsBackEverything(t *testing.T) {
	st := openTemp(t)
	seed(t, st)
	before := counts(t, st)
	snap, _ := st.ExportSnapshot(t.Context(), "full", 0)
	for _, step := range []string{"cards", "conversations"} {
		boom := errors.New("boom")
		importFailpoint = func(s string) error {
			if s == step {
				return boom
			}
			return nil
		}
		_, err := st.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "add"})
		importFailpoint = nil
		if !errors.Is(err, boom) {
			t.Fatalf("%s: %v", step, err)
		}
		if counts(t, st) != before {
			t.Fatalf("%s: partial import persisted", step)
		}
	}
}

func TestSkipPolicyReusesExistingCardsAndDefaultsAreOptIn(t *testing.T) {
	st := openTemp(t)
	seed(t, st)
	snap, _ := st.ExportSnapshot(t.Context(), "full", 0)
	snap.Defaults = &interchange.SnapDefaults{Character: snap.Cards[0].Key}
	res, err := st.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "skip"})
	if err != nil || res.CardsSkipped != 3 || res.CardsAdded != 0 || res.ConversationsAdded != 1 || res.DefaultsApplied {
		t.Fatalf("%v %+v", err, res)
	}
	if n := counts(t, st); n[0] != 3 {
		t.Fatalf("cards duplicated: %v", n)
	}
	d, _ := st.GetCast(t.Context(), 0)
	if d.CharacterID != nil {
		t.Fatal("defaults applied without opt-in")
	}
	res, err = st.ImportSnapshot(t.Context(), snap, ImportOptions{Policy: "skip", ApplyDefaults: true})
	d, _ = st.GetCast(t.Context(), 0)
	if err != nil || !res.DefaultsApplied || d.CharacterID == nil {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestNeverExportsEndpointsOrCredentials(t *testing.T) {
	st := openTemp(t)
	seed(t, st)
	ep, err := st.CreateEndpoint(t.Context(), "Secret host", "openai", "http://127.0.0.1:1", "local", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.ExecContext(t.Context(), `INSERT INTO endpoint_credentials(endpoint_id,protected_blob,created_at) VALUES(?,?,?)`, ep.ID, []byte("SECRET-BLOB"), "x"); err != nil {
		t.Fatal(err)
	}
	snap, _ := st.ExportSnapshot(t.Context(), "full", 0)
	raw := marshalForTest(t, snap)
	for _, forbidden := range []string{"SECRET-BLOB", "Secret host", "127.0.0.1:1", "secret", "endpoint"} {
		if containsFold(raw, forbidden) {
			t.Fatalf("snapshot leaks %q", forbidden)
		}
	}
}

func TestResetScopesPreserveEndpointsAndCredentials(t *testing.T) {
	for _, scope := range []string{"conversations", "cast", "all"} {
		st := openTemp(t)
		seed(t, st)
		ctx := context.Background()
		ep, _ := st.CreateEndpoint(ctx, "E", "openai", "http://127.0.0.1:1", "local", 1)
		st.db.ExecContext(ctx, `INSERT INTO endpoint_credentials(endpoint_id,protected_blob,created_at) VALUES(?,?,?)`, ep.ID, []byte("b"), "x")
		if err := st.ResetData(ctx, scope); err != nil {
			t.Fatal(err)
		}
		n := counts(t, st)
		wantCards, wantConvs := 3, 1
		if scope != "conversations" {
			wantCards = 0
		}
		if scope != "cast" {
			wantConvs = 0
		}
		if n[0] != wantCards || n[1] != wantConvs || n[4] != 1 {
			t.Fatalf("%s: %v", scope, n)
		}
		var creds int
		st.db.QueryRow(`SELECT COUNT(*) FROM endpoint_credentials`).Scan(&creds)
		if creds != 1 {
			t.Fatalf("%s: credentials lost", scope)
		}
		if scope == "cast" { // historical speakers survive deletion of the library
			msgs, _ := st.ListMessages(ctx, 1)
			if len(msgs) != 2 || msgs[1].SpeakerJSON == "{}" {
				t.Fatalf("speaker snapshot lost: %+v", msgs)
			}
		}
	}
	if err := openTemp(t).ResetData(t.Context(), "endpoints"); !errors.Is(err, ErrInvalidImport) {
		t.Fatal("unknown scope accepted")
	}
}

func marshalForTest(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
