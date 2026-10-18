package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/interchange"
)

func TestSnapshotRoundTripRetainsBranchesVariantsAndOrganization(t *testing.T) {
	src := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, src)
	edited, err := src.EditMessage(ctx, cid, m[2].ID, "q2b", revisionOf(t, src, cid))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = src.CreateMessage(ctx, cid, nil, "assistant", "a2b", "", "complete"); err != nil {
		t.Fatal(err)
	}
	f, _ := src.CreateFolder(ctx, "Work")
	yes := true
	if err = src.SetOrganization(ctx, cid, Organization{Pinned: &yes, FolderID: &f.ID, Tags: &[]string{"x", "Y"}}); err != nil {
		t.Fatal(err)
	}
	snap, err := src.ExportSnapshot(ctx, "conversation", cid)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Conversations[0].Messages) != 6 {
		t.Fatalf("every branch must be exported, got %d messages", len(snap.Conversations[0].Messages))
	}
	// Through the real parser, as an uploaded file would arrive.
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := interchange.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	dst := openTemp(t)
	res, err := dst.ImportSnapshot(ctx, parsed.Snapshot, ImportOptions{Policy: "add"})
	if err != nil || res.MessagesAdded != 6 || res.FoldersAdded != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	convs, _ := dst.ListConversations(ctx)
	got := convs[0]
	if !got.Pinned || got.FolderID == nil || len(got.Tags) != 2 {
		t.Fatalf("organization lost: %+v", got)
	}
	p, _, _ := dst.ActivePath(ctx, got.ID)
	if contents(p) != "q1|a1|q2b|a2b" {
		t.Fatalf("visible branch changed: %s", contents(p))
	}
	if len(p[2].VariantIDs) != 2 || p[2].Origin != "edit" || p[2].OriginID == nil {
		t.Fatalf("variant relationship lost: %+v %v", p[2].Message, p[2].VariantIDs)
	}
	// The other variant keeps its own continuation after the round trip.
	if err = dst.ActivateMessage(ctx, got.ID, p[2].VariantIDs[0], revisionOf(t, dst, got.ID)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = dst.ActivePath(ctx, got.ID)
	if contents(p) != "q1|a1|q2|a2" {
		t.Fatalf("variant continuation lost: %s", contents(p))
	}
	_ = edited
}

func TestLegacyLinearSnapshotStillImports(t *testing.T) {
	legacy := []byte(`{"format":"labbonet-snapshot","version":1,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"Old","messages":[{"role":"user","content":"hi","status":"complete"},{"role":"assistant","content":"yo","status":"complete"}]}]}`)
	parsed, err := interchange.Parse(legacy)
	if err != nil {
		t.Fatal(err)
	}
	st := openTemp(t)
	if _, err = st.ImportSnapshot(t.Context(), parsed.Snapshot, ImportOptions{Policy: "add"}); err != nil {
		t.Fatal(err)
	}
	convs, _ := st.ListConversations(t.Context())
	p, _, _ := st.ActivePath(t.Context(), convs[0].ID)
	if contents(p) != "hi|yo" {
		t.Fatal(contents(p))
	}
}

func TestMalformedTreeSnapshotsAreRejectedWithoutWrites(t *testing.T) {
	base := func() string {
		return `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","active":"b","messages":[` +
			`{"key":"a","role":"user","content":"x","status":"complete"},` +
			`{"key":"b","parent":"a","role":"assistant","content":"y","status":"complete"}]}]}`
	}
	if _, err := interchange.Parse([]byte(base())); err != nil {
		t.Fatal(err)
	}
	for name, doc := range map[string]string{
		"parent after child": `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","parent":"b","role":"user","content":"x","status":"complete"},{"key":"b","role":"user","content":"y","status":"complete"}]}]}`,
		"self parent":        `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","parent":"a","role":"user","content":"x","status":"complete"}]}]}`,
		"duplicate key":      `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","role":"user","content":"x","status":"complete"},{"key":"a","role":"user","content":"y","status":"complete"}]}]}`,
		"mixed keys":         `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","role":"user","content":"x","status":"complete"},{"role":"user","content":"y","status":"complete"}]}]}`,
		"selected not child": `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","selected":"b","role":"user","content":"x","status":"complete"},{"key":"b","role":"user","content":"y","status":"complete"}]}]}`,
		"unknown active":     `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","active":"zzz","messages":[{"key":"a","role":"user","content":"x","status":"complete"}]}]}`,
		"unknown origin":     `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","messages":[{"key":"a","origin":"magic","role":"user","content":"x","status":"complete"}]}]}`,
		"too many tags":      `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","tags":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17"],"messages":[]}]}`,
		"unknown field":      `{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","endpoint_id":4,"messages":[]}]}`,
	} {
		if _, err := interchange.Parse([]byte(doc)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A tag that passes the parser but not the store's normalization rolls back whole.
	st := openTemp(t)
	parsed, err := interchange.Parse([]byte(`{"format":"labbonet-snapshot","version":2,"scope":"conversation","cards":[],"conversations":[{"key":"t1","title":"T","tags":["ok","bad\u0007tag"],"messages":[{"role":"user","content":"x","status":"complete"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	before := counts(t, st)
	if _, err = st.ImportSnapshot(t.Context(), parsed.Snapshot, ImportOptions{Policy: "add"}); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("control character tag: %v", err)
	}
	if counts(t, st) != before {
		t.Fatal("rejected import changed the database")
	}
}

func TestResetConversationsAlsoClearsFolders(t *testing.T) {
	st := openTemp(t)
	chat(t, st)
	if _, err := st.CreateFolder(t.Context(), "F"); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetData(t.Context(), "conversations"); err != nil {
		t.Fatal(err)
	}
	if fs, _ := st.ListFolders(t.Context()); len(fs) != 0 {
		t.Fatal("folders must be cleared with the threads they organized")
	}
}
