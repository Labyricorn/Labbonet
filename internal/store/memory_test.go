package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Labyricorn/Labbonet/internal/memory"
)

func TestMemorySettingsDefaultsValidationAndRevision(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	cur, err := st.GetMemorySettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Revision != 1 || !cur.RetrievalEnabled || !cur.RetrieverB || cur.RetrieverA || cur.SummarizeEnabled {
		t.Fatalf("defaults = %+v", cur)
	}
	bad := cur
	bad.FireThreshold = 0
	if _, err = st.SaveMemorySettings(ctx, bad); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("zero threshold accepted: %v", err)
	}
	bad = cur
	bad.RetrieverA = true // no embedding model chosen
	if _, err = st.SaveMemorySettings(ctx, bad); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("semantic without model accepted: %v", err)
	}

	ep, err := st.CreateEndpoint(ctx, "lm", "openai", "http://127.0.0.1:1", "local", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []Model{{EndpointID: ep.ID, ModelID: "chat-m", Type: "llm", State: "loaded", SeenAt: time.Now()}, {EndpointID: ep.ID, ModelID: "embed-m", Type: "embeddings", State: "loaded", SeenAt: time.Now()}} {
		if err = st.UpsertModel(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// An embedding model cannot write summaries, a chat model cannot embed, and an
	// unknown model is refused.
	for _, c := range []struct {
		compress, embed string
	}{{"embed-m", ""}, {"", "chat-m"}, {"missing", ""}} {
		s := cur
		if c.compress != "" {
			s.CompressionEndpointID, s.CompressionModel = &ep.ID, c.compress
		}
		if c.embed != "" {
			s.EmbeddingEndpointID, s.EmbeddingModel = &ep.ID, c.embed
		}
		if _, err = st.SaveMemorySettings(ctx, s); !errors.Is(err, ErrInvalidMemory) {
			t.Fatalf("%+v accepted: %v", c, err)
		}
	}
	good := cur
	good.CompressionEndpointID, good.CompressionModel = &ep.ID, "chat-m"
	good.EmbeddingEndpointID, good.EmbeddingModel = &ep.ID, "embed-m"
	good.RetrieverA, good.SummarizeEnabled = true, true
	saved, err := st.SaveMemorySettings(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 || !saved.RetrieverA || saved.EmbeddingModel != "embed-m" {
		t.Fatalf("saved = %+v", saved)
	}
	if _, err = st.SaveMemorySettings(ctx, good); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
}

func TestSummariesAreAppendOnlyBranchAwareAndConflictChecked(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	convID, m := chat(t, st)
	seq, err := st.LatestSummarySeq(ctx, convID)
	if err != nil || seq != 0 {
		t.Fatalf("seq=%d err=%v", seq, err)
	}
	through := m[1].ID
	first, err := st.AddSummary(ctx, Summary{ConversationID: convID, Text: "S1", ThroughMessageID: &through, AnchorMessageID: &through, ArchivedCount: 2, Source: "auto"}, 0)
	if err != nil || first.Seq != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	// A writer that read before the first revision cannot overwrite it.
	if _, err = st.AddSummary(ctx, Summary{ConversationID: convID, Text: "late", Source: "manual"}, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write accepted: %v", err)
	}
	// A failed pass is recorded but never becomes the effective summary.
	if _, err = st.AddSummary(ctx, Summary{ConversationID: convID, Source: "failed", Note: "model returned nothing"}, 1); err != nil {
		t.Fatal(err)
	}
	onPath := map[int64]int{m[0].ID: 0, m[1].ID: 1, m[2].ID: 2, m[3].ID: 3}
	eff, err := st.EffectiveSummary(ctx, convID, onPath)
	if err != nil || eff == nil || eff.Text != "S1" {
		t.Fatalf("effective=%+v err=%v", eff, err)
	}
	// On a branch that does not contain the archived message the summary is not used.
	other := map[int64]int{m[0].ID: 0, 9999: 1}
	if eff, err = st.EffectiveSummary(ctx, convID, other); err != nil || eff != nil {
		t.Fatalf("summary leaked onto another branch: %+v err=%v", eff, err)
	}
	// A message from another conversation cannot be an anchor.
	otherConv, om := chat(t, st)
	_ = otherConv
	if _, err = st.AddSummary(ctx, Summary{ConversationID: convID, Text: "x", ThroughMessageID: &om[0].ID, Source: "manual"}, 2); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("foreign message accepted: %v", err)
	}
	list, err := st.ListSummaries(ctx, convID, 10)
	if err != nil || len(list) != 2 || list[0].Source != "failed" || list[1].Text != "S1" {
		t.Fatalf("history=%+v err=%v", list, err)
	}
	// Source history is untouched by summaries.
	msgs, _ := st.ListMessages(ctx, convID)
	if len(msgs) != 4 {
		t.Fatalf("messages changed: %d", len(msgs))
	}
	// Deleting the conversation removes its summaries and records.
	if err = st.AddRetrievalRecord(ctx, convID, nil, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteConversation(ctx, convID); err != nil {
		t.Fatal(err)
	}
	var n int
	st.db.QueryRow(`SELECT (SELECT COUNT(*) FROM conversation_summaries WHERE conversation_id=?)+(SELECT COUNT(*) FROM retrieval_records WHERE conversation_id=?)`, convID, convID).Scan(&n)
	if n != 0 {
		t.Fatalf("%d orphan rows", n)
	}
}

func TestEmbeddingCacheKeysOnModelAndContent(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	vec := []float32{0.1, 0.2, 0.3}
	if err := st.PutEmbeddings(ctx, 1, "model-a", map[string][]float32{"h1": vec}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEmbeddings(ctx, 1, "model-a", []string{"h1", "h2"})
	if err != nil || len(got) != 1 || len(got["h1"]) != 3 || got["h1"][2] != 0.3 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	// A different model or endpoint never sees another model's vector.
	for _, c := range []struct {
		ep    int64
		model string
	}{{1, "model-b"}, {2, "model-a"}} {
		if got, _ = st.GetEmbeddings(ctx, c.ep, c.model, []string{"h1"}); len(got) != 0 {
			t.Fatalf("%v reused %v", c, got)
		}
	}
	if err = st.PutEmbeddings(ctx, 1, "model-a", map[string][]float32{"bad": {}}); !errors.Is(err, ErrInvalidMemory) {
		t.Fatalf("empty vector accepted: %v", err)
	}
	stats, _ := st.EmbeddingCacheStats(ctx)
	if len(stats) != 1 || stats[0].Vectors != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	if n, err := st.ClearEmbeddingCache(ctx); err != nil || n != 1 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
}

func TestRetrievalRecordsAreBounded(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	convID, _ := chat(t, st)
	for i := 0; i < retrievalPerConv+10; i++ {
		if err := st.AddRetrievalRecord(ctx, convID, nil, map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.ListRetrievalRecords(ctx, convID, 100)
	if err != nil || len(rows) != 20 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM retrieval_records`).Scan(&n)
	if n != retrievalPerConv {
		t.Fatalf("stored %d, want %d", n, retrievalPerConv)
	}
	_ = memory.Cost
}

// A populated schema-3 database upgrades to schema 4 with its data intact and a
// pre-upgrade backup, and the memory tables start empty with default settings.
func TestMemoryMigrationPreservesExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []migrationFile{{1, "0001_init.sql"}, {2, "0002_cast.sql"}, {3, "0003_branches.sql"}} {
		if err = applyMigration(db, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO conversations(title,params_json,created_at,updated_at) VALUES('keep','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	convs, err := st.ListConversations(t.Context())
	if err != nil || len(convs) != 1 || convs[0].Title != "keep" {
		t.Fatalf("convs=%+v err=%v", convs, err)
	}
	if backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "*")); len(backups) != 1 {
		t.Fatalf("pre-upgrade backup missing: %v", backups)
	}
	s, err := st.GetMemorySettings(t.Context())
	if err != nil || s.Revision != 1 || s.FireThreshold != 1 {
		t.Fatalf("settings=%+v err=%v", s, err)
	}
}
