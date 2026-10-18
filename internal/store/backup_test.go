package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A-B8: backup then restore (copy the backup over the database) gives an
// identical conversation set, with the credential table empty in the
// backup.
func TestBackup_RestoreGivesIdenticalConversationsAndEmptyCredentials(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "labbonet.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	ep, err := st.CreateEndpoint(ctx, "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := st.SetCredential(ctx, ep.ID, []byte("protected-blob")); err != nil {
		t.Fatalf("set credential: %v", err)
	}

	conv, err := st.CreateConversation(ctx, "Test chat", &ep.ID, nil, "", "{}")
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := st.CreateMessage(ctx, conv.ID, nil, "user", "hello", "", "complete"); err != nil {
		t.Fatalf("create message: %v", err)
	}

	backupPath, err := st.Backup(ctx, dir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	wantConvs, err := st.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list conversations before restore: %v", err)
	}
	wantMsgs, err := st.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("list messages before restore: %v", err)
	}

	st.Close()

	// Restore: copy the backup over the live database file.
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("restore backup over database: %v", err)
	}
	// SQLite WAL/SHM siblings from the pre-backup state must not leak into
	// the restored file's view.
	os.Remove(path + "-wal")
	os.Remove(path + "-shm")

	restored, err := Open(path)
	if err != nil {
		t.Fatalf("open restored database: %v", err)
	}
	defer restored.Close()

	gotConvs, err := restored.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list conversations after restore: %v", err)
	}
	if len(gotConvs) != len(wantConvs) || len(gotConvs) != 1 {
		t.Fatalf("restored conversations = %d, want %d", len(gotConvs), len(wantConvs))
	}
	if gotConvs[0].Title != wantConvs[0].Title {
		t.Fatalf("restored conversation title = %q, want %q", gotConvs[0].Title, wantConvs[0].Title)
	}

	gotMsgs, err := restored.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("list messages after restore: %v", err)
	}
	if len(gotMsgs) != len(wantMsgs) || len(gotMsgs) != 1 {
		t.Fatalf("restored messages = %d, want %d", len(gotMsgs), len(wantMsgs))
	}
	if gotMsgs[0].Content != wantMsgs[0].Content {
		t.Fatalf("restored message content = %q, want %q", gotMsgs[0].Content, wantMsgs[0].Content)
	}

	has, err := restored.HasCredential(ctx, ep.ID)
	if err != nil {
		t.Fatalf("has credential after restore: %v", err)
	}
	if has {
		t.Fatalf("restored database (from backup) still has a credential; DR-016 requires the backup's credential table to be empty")
	}
}

func TestBackup_WritesIntoBackupsDir(t *testing.T) {
	ctx := t.Context()
	st, dir := newTestStore(t)

	if _, err := st.Backup(ctx, dir); err != nil {
		t.Fatalf("first backup: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("backups dir has %d entries, want 1", len(entries))
	}
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "labbonet.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, dir
}
