package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func ids(path []PathMessage) []int64 {
	out := []int64{}
	for _, m := range path {
		out = append(out, m.ID)
	}
	return out
}

func contents(path []PathMessage) string {
	var parts []string
	for _, m := range path {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "|")
}

// chat builds user "q1" > assistant "a1" > user "q2" > assistant "a2".
func chat(t *testing.T, st *Store) (convID int64, m [4]*Message) {
	t.Helper()
	ctx := t.Context()
	conv, err := st.CreateConversation(ctx, "T", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []struct{ role, text string }{{"user", "q1"}, {"assistant", "a1"}, {"user", "q2"}, {"assistant", "a2"}} {
		if m[i], err = st.CreateMessage(ctx, conv.ID, nil, c.role, c.text, "", "complete"); err != nil {
			t.Fatal(err)
		}
	}
	return conv.ID, m
}

func revisionOf(t *testing.T, st *Store, id int64) int64 {
	t.Helper()
	c, err := st.GetConversation(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return c.Revision
}

func TestLegacyMessagesBecomeOneBranch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []migrationFile{{1, "0001_init.sql"}, {2, "0002_cast.sql"}} {
		if err = applyMigration(db, f); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO conversations(id,title,created_at,updated_at) VALUES(1,'a','2026-10-18T00:00:00Z','2026-10-18T00:00:00Z'),(2,'b','2026-10-18T00:00:00Z','2026-10-18T00:00:00Z')`,
		`INSERT INTO messages(id,conversation_id,role,content,status,created_at) VALUES(1,1,'user','one','complete','2026-10-18T00:00:00Z'),(2,2,'user','other','complete','2026-10-18T00:00:00Z'),(3,1,'assistant','two','complete','2026-10-18T00:00:00Z'),(4,1,'user','three','complete','2026-10-18T00:00:00Z')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	p, rev, err := st.ActivePath(t.Context(), 1)
	if err != nil || rev != 1 {
		t.Fatal(err, rev)
	}
	if contents(p) != "one|two|three" {
		t.Fatalf("legacy history must stay one linear branch, got %s", contents(p))
	}
	other, _, _ := st.ActivePath(t.Context(), 2)
	if contents(other) != "other" || other[0].ParentID != nil {
		t.Fatal("conversations must not mix", contents(other))
	}
}

func TestCreateMessageFollowsTheVisibleBranch(t *testing.T) {
	st := openTemp(t)
	cid, m := chat(t, st)
	p, _, err := st.ActivePath(t.Context(), cid)
	if err != nil || contents(p) != "q1|a1|q2|a2" {
		t.Fatal(contents(p), err)
	}
	if m[1].ParentID == nil || *m[1].ParentID != m[0].ID || m[0].ParentID != nil {
		t.Fatal("messages must chain", m[1].ParentID)
	}
}

func TestEditCreatesVariantAndKeepsOriginalContinuation(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	rev := revisionOf(t, st, cid)
	edited, err := st.EditMessage(ctx, cid, m[2].ID, "q2 edited", rev)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Origin != "edit" || edited.OriginID == nil || *edited.OriginID != m[2].ID || *edited.ParentID != m[1].ID {
		t.Fatalf("edit provenance wrong: %+v", edited)
	}
	p, _, _ := st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q2 edited" {
		t.Fatalf("visible branch should end at the edit, got %s", contents(p))
	}
	if len(p[2].VariantIDs) != 2 || p[2].VariantIDs[0] != m[2].ID || p[2].VariantIDs[1] != edited.ID {
		t.Fatalf("variants = %v", p[2].VariantIDs)
	}
	// The original keeps its own continuation and the original text.
	orig, err := st.GetMessage(ctx, m[2].ID)
	if err != nil || orig.Content != "q2" {
		t.Fatal("original text must be immutable", orig, err)
	}
	if err = st.ActivateMessage(ctx, cid, m[2].ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q2|a2" {
		t.Fatalf("switching back must restore the variant's own continuation, got %s", contents(p))
	}
	// Stale revision conflicts and writes nothing.
	if _, err = st.EditMessage(ctx, cid, m[0].ID, "stale", rev); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	all, _ := st.ListMessages(ctx, cid)
	if len(all) != 5 {
		t.Fatalf("stale edit persisted: %d messages", len(all))
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", maxMessageBytes+1)} {
		if _, err = st.EditMessage(ctx, cid, m[0].ID, bad, revisionOf(t, st, cid)); !errors.Is(err, ErrInvalidMessage) {
			t.Fatalf("invalid edit %.10q: %v", bad, err)
		}
	}
	if _, err = st.EditMessage(ctx, cid, 9999, "x", 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing message: %v", err)
	}
	other, _ := st.CreateConversation(ctx, "other", nil, nil, "", "{}")
	if _, err = st.EditMessage(ctx, other.ID, m[0].ID, "x", 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a message of another conversation must not be editable: %v", err)
	}
}

func TestRerollVariantsKeepSeparateContinuations(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	ep, _ := st.CreateEndpoint(ctx, "e", "openai", "http://127.0.0.1:1", "local", 1)
	sel, _ := st.GetCast(ctx, cid)
	// Reroll a1: a new assistant below q1.
	if err := st.UpdateMessageContent(ctx, m[3].ID, "a2", "", "complete"); err != nil {
		t.Fatal(err)
	}
	msgID, _, err := st.StartTurn(ctx, Turn{ConversationID: cid, EndpointID: ep.ID, Model: "m", ParentID: &m[0].ID, AssistantSpeaker: "{}", Params: "{}", CastRevision: sel.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpdateMessageContent(ctx, msgID, "a1 reroll", "", "complete"); err != nil {
		t.Fatal(err)
	}
	re, _ := st.GetMessage(ctx, msgID)
	if re.Origin != "reroll" || re.OriginID == nil || *re.OriginID != m[1].ID {
		t.Fatalf("reroll provenance: %+v", re)
	}
	p, _, _ := st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1 reroll" || len(p[1].VariantIDs) != 2 {
		t.Fatal(contents(p), p[1].VariantIDs)
	}
	// Continue the new variant, then flip back and forth.
	sel, _ = st.GetCast(ctx, cid)
	leaf := msgID
	text := "q-new"
	n, _, err := st.StartTurn(ctx, Turn{ConversationID: cid, EndpointID: ep.ID, Model: "m", User: &text, ExpectedLeaf: &leaf, UserSpeaker: "{}", AssistantSpeaker: "{}", Params: "{}", CastRevision: sel.Revision})
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateMessageContent(ctx, n, "a-new", "", "complete")
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1 reroll|q-new|a-new" {
		t.Fatal(contents(p))
	}
	if err = st.ActivateMessage(ctx, cid, m[1].ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q2|a2" {
		t.Fatalf("original variant lost its continuation: %s", contents(p))
	}
	if err = st.ActivateMessage(ctx, cid, msgID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1 reroll|q-new|a-new" {
		t.Fatalf("reroll variant lost its continuation: %s", contents(p))
	}
	// Regeneration must answer a user message.
	sel, _ = st.GetCast(ctx, cid)
	if _, _, err = st.StartTurn(ctx, Turn{ConversationID: cid, EndpointID: ep.ID, Model: "m", ParentID: &m[1].ID, AssistantSpeaker: "{}", Params: "{}", CastRevision: sel.Revision}); !errors.Is(err, ErrConflict) {
		t.Fatalf("regenerating below an assistant: %v", err)
	}
}

func TestDeleteMessageRemovesSubtreeAndRepairsBranch(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	edited, err := st.EditMessage(ctx, cid, m[2].ID, "q2b", revisionOf(t, st, cid))
	if err != nil {
		t.Fatal(err)
	}
	// Delete the edited variant: the original q2>a2 returns.
	if err = st.DeleteMessage(ctx, cid, edited.ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q2|a2" || len(p[2].VariantIDs) != 1 {
		t.Fatal(contents(p), p[2].VariantIDs)
	}
	// Delete q2 (no siblings): its reply goes too and the branch ends at a1.
	if err = st.DeleteMessage(ctx, cid, m[2].ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1" {
		t.Fatal(contents(p))
	}
	all, _ := st.ListMessages(ctx, cid)
	if len(all) != 2 {
		t.Fatalf("subtree must be deleted, %d left", len(all))
	}
	// New messages continue from the repaired tip.
	if _, err = st.CreateMessage(ctx, cid, nil, "user", "q3", "", "complete"); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q3" {
		t.Fatal(contents(p))
	}
	// Delete the root: nothing is left.
	if err = st.DeleteMessage(ctx, cid, m[0].ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ = st.ActivePath(ctx, cid)
	if len(p) != 0 {
		t.Fatal(contents(p))
	}
	if err = st.DeleteMessage(ctx, cid, m[0].ID, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestDeleteInactiveVariantLeavesVisibleBranch(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	edited, _ := st.EditMessage(ctx, cid, m[2].ID, "q2b", revisionOf(t, st, cid))
	// Visible branch is the edit; delete the original (inactive) variant and its reply.
	if err := st.DeleteMessage(ctx, cid, m[2].ID, revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.ActivePath(ctx, cid)
	if contents(p) != "q1|a1|q2b" || p[2].ID != edited.ID {
		t.Fatal(contents(p))
	}
	all, _ := st.ListMessages(ctx, cid)
	if len(all) != 3 {
		t.Fatalf("%d messages", len(all))
	}
}

func TestTreeEditsRefusedWhileStreaming(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	if err := st.UpdateMessageContent(ctx, m[3].ID, "partial", "", "streaming"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EditMessage(ctx, cid, m[0].ID, "x", 0); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := st.DeleteMessage(ctx, cid, m[0].ID, 0); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := st.ActivateMessage(ctx, cid, m[0].ID, 0); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	all, _ := st.ListMessages(ctx, cid)
	if len(all) != 4 {
		t.Fatal("refused edits must not write")
	}
}

func TestForkCopiesChainWithCastAndProvenance(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	cid, m := chat(t, st)
	if _, err := st.EditMessage(ctx, cid, m[2].ID, "q2b", revisionOf(t, st, cid)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOrganization(ctx, cid, Organization{Tags: &[]string{"keep"}}); err != nil {
		t.Fatal(err)
	}
	fork, err := st.ForkConversation(ctx, cid, m[1].ID, "Fork")
	if err != nil {
		t.Fatal(err)
	}
	p, _, _ := st.ActivePath(ctx, fork.ID)
	if contents(p) != "q1|a1" || p[0].Origin != "fork" || p[0].OriginID == nil || *p[0].OriginID != m[0].ID {
		t.Fatalf("fork path %s %+v", contents(p), p[0].Message)
	}
	if fork.ForkedFromConversationID == nil || *fork.ForkedFromConversationID != cid || len(fork.Tags) != 1 {
		t.Fatalf("fork provenance %+v", fork)
	}
	// Source untouched; fork edits stay in the fork.
	src, _, _ := st.ActivePath(ctx, cid)
	if contents(src) != "q1|a1|q2b" {
		t.Fatal(contents(src))
	}
	if _, err = st.CreateMessage(ctx, fork.ID, nil, "user", "fork only", "", "complete"); err != nil {
		t.Fatal(err)
	}
	src2, _, _ := st.ActivePath(ctx, cid)
	if contents(src2) != "q1|a1|q2b" {
		t.Fatal("fork leaked into the source")
	}
	if _, err = st.ForkConversation(ctx, cid, 9999, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	// A message of another conversation cannot seed a fork.
	if _, err = st.ForkConversation(ctx, fork.ID, m[0].ID, "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	c := context.Background()
	var cast int
	if err = st.db.QueryRowContext(c, `SELECT COUNT(*) FROM conversation_cast WHERE conversation_id=?`, fork.ID).Scan(&cast); err != nil || cast != 1 {
		t.Fatal("fork needs its own cast row", cast, err)
	}
}

func TestFolderReorderAtomicAndStale(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	a, _ := st.CreateFolder(ctx, "A")
	b, _ := st.CreateFolder(ctx, "B")
	c, _ := st.CreateFolder(ctx, "C")
	before := []int64{a.ID, b.ID, c.ID}
	after := []int64{c.ID, a.ID, b.ID}
	if err := st.ReorderFolders(ctx, before, []int64{a.ID, a.ID, c.ID}); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatal(err)
	}
	if err := st.ReorderFolders(ctx, before, after); err != nil {
		t.Fatal(err)
	}
	if err := st.ReorderFolders(ctx, before, before); !errors.Is(err, ErrConflict) {
		t.Fatal("stale order accepted", err)
	}
	if err := st.ReorderFolders(ctx, after, []int64{a.ID, b.ID, 999}); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatal(err)
	}
	got, _ := st.ListFolders(ctx)
	for i, f := range got {
		if f.ID != after[i] {
			t.Fatal("partial reorder", got)
		}
	}
	_, _ = st.CreateFolder(ctx, "D")
	if err := st.ReorderFolders(ctx, after, after); !errors.Is(err, ErrConflict) {
		t.Fatal("missing new folder accepted", err)
	}
}

func TestOrganizationFoldersTagsPinsOrderAndSearch(t *testing.T) {
	st := openTemp(t)
	ctx := t.Context()
	a, _ := st.CreateConversation(ctx, "Alpha", nil, nil, "", "{}")
	b, _ := st.CreateConversation(ctx, "Beta", nil, nil, "", "{}")
	c, _ := st.CreateConversation(ctx, "Gamma 100%", nil, nil, "", "{}")
	st.CreateMessage(ctx, b.ID, nil, "user", "the hidden needle is here", "", "complete")
	st.CreateMessage(ctx, c.ID, nil, "user", "plain", "", "complete")

	f, err := st.CreateFolder(ctx, "  Work   items ")
	if err != nil || f.Name != "Work items" {
		t.Fatal(f, err)
	}
	if _, err = st.CreateFolder(ctx, "work ITEMS"); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatal("folder names must be unique ignoring case", err)
	}
	if _, err = st.CreateFolder(ctx, "bad\x07name"); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatal(err)
	}
	yes := true
	tags := []string{"Draft", "draft", "#ideas"}
	if err = st.SetOrganization(ctx, a.ID, Organization{Pinned: &yes, FolderID: &f.ID, Tags: &tags}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetConversation(ctx, a.ID)
	if !got.Pinned || got.FolderID == nil || len(got.Tags) != 2 {
		t.Fatalf("%+v", got)
	}
	for _, bad := range []Organization{
		{FolderID: &[]int64{999}[0]},
		{Tags: &[]string{""}},
		{Tags: &[]string{strings.Repeat("x", 33)}},
		{FolderID: &f.ID, ClearFolder: true},
	} {
		if err = st.SetOrganization(ctx, a.ID, bad); !errors.Is(err, ErrInvalidOrganization) {
			t.Fatalf("%+v accepted: %v", bad, err)
		}
	}
	if err = st.SetOrganization(ctx, 999, Organization{Pinned: &yes}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	// Pinned first, then newest.
	list, _ := st.ListConversations(ctx)
	if list[0].ID != a.ID {
		t.Fatal("pinned thread must lead")
	}
	// Manual order is atomic and must name every conversation once.
	if err = st.ReorderConversations(ctx, []int64{c.ID, b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListConversations(ctx)
	if list[0].ID != a.ID || list[1].ID != c.ID || list[2].ID != b.ID {
		t.Fatalf("order: %d %d %d", list[0].ID, list[1].ID, list[2].ID)
	}
	if err = st.ReorderConversations(ctx, []int64{a.ID, b.ID}); !errors.Is(err, ErrConflict) {
		t.Fatal("incomplete order must conflict", err)
	}
	if err = st.ReorderConversations(ctx, []int64{a.ID, a.ID, b.ID}); err == nil {
		t.Fatal("duplicate ids accepted")
	}
	list, _ = st.ListConversations(ctx)
	if list[1].ID != c.ID {
		t.Fatal("failed reorder changed the order")
	}
	// Search covers title, tag and any branch's message text; wildcards are literal.
	for term, want := range map[string]int64{"alpha": a.ID, "needle": b.ID, "IDEAS": a.ID, "100%": c.ID} {
		hits, err := st.SearchConversations(ctx, term)
		if err != nil || len(hits) != 1 || hits[0].ConversationID != want {
			t.Fatalf("search %q: %+v %v", term, hits, err)
		}
	}
	if hits, _ := st.SearchConversations(ctx, "%%"); len(hits) != 0 {
		t.Fatal("wildcards must not match everything", hits)
	}
	if _, err = st.SearchConversations(ctx, "x"); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatal(err)
	}
	// Deleting a folder unfiles its threads without deleting them.
	if err = st.DeleteFolder(ctx, f.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetConversation(ctx, a.ID)
	if got.FolderID != nil {
		t.Fatal("folder delete must unfile")
	}
	// Edited-away text from a non-visible branch is still searchable.
	st.CreateMessage(ctx, c.ID, nil, "assistant", "reply", "", "complete")
	p, rev, _ := st.ActivePath(ctx, c.ID)
	if _, err = st.EditMessage(ctx, c.ID, p[0].ID, "rewritten", rev); err != nil {
		t.Fatal(err)
	}
	if hits, _ := st.SearchConversations(ctx, "plain"); len(hits) != 1 {
		t.Fatal("inactive variants must be searchable", hits)
	}
}
