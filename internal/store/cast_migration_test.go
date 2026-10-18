package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func legacyDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err = applyMigration(db, migrationFile{version: 1, name: "0001_init.sql"}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO endpoints(id,name,provider_type,base_url,privacy_class,created_at) VALUES(1,'legacy','openai','http://127.0.0.1:1','local','2026-10-18T00:00:00Z')`,
		`INSERT INTO endpoint_credentials VALUES(1,x'1234567890abcdef','2026-10-18T00:00:00Z')`,
		`INSERT INTO conversations(id,title,endpoint_id,model_id,created_at,updated_at) VALUES(1,'legacy',1,'m','2026-10-18T00:00:00Z','2026-10-18T00:00:00Z')`,
		`INSERT INTO messages(id,conversation_id,role,content,status,created_at) VALUES(1,1,'assistant','history','complete','2026-10-18T00:00:00Z')`,
		`INSERT INTO generations(message_id,endpoint_id,model_requested,params_json,started_at) VALUES(1,1,'m','{"legacy":true}','2026-10-18T00:00:00Z')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return path, db
}

func TestCastUpgradePreservesLiveDataAndRedactsPreUpgradeBackup(t *testing.T) {
	path, db := legacyDatabase(t)
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	blob, err := st.CredentialBlob(t.Context(), 1)
	if err != nil || !bytes.Equal(blob, []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0xab, 0xcd, 0xef}) {
		t.Fatal(blob, err)
	}
	msgs, err := st.ListMessages(t.Context(), 1)
	if err != nil || len(msgs) != 1 || msgs[0].Content != "history" || msgs[0].SpeakerJSON != "{}" {
		t.Fatal(msgs, err)
	}
	gens, err := st.ListGenerationsForMessage(t.Context(), 1)
	if err != nil || len(gens) != 1 || gens[0].ParamsJSON != `{"legacy":true}` {
		t.Fatal(gens, err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "*.db"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	backup, err := sql.Open("sqlite", files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var n int
	if err = backup.QueryRow(`SELECT COUNT(*) FROM endpoint_credentials`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if rawUserVersion(t, backup) != 1 {
		t.Fatal("backup must be pre-upgrade")
	}
	raw, err := os.ReadFile(files[0])
	if err != nil || bytes.Contains(raw, blob) {
		t.Fatal("credential bytes in backup", err)
	}
}

func TestFailedCastMigrationRollsBack(t *testing.T) {
	_, db := legacyDatabase(t)
	defer db.Close()
	// Force the second CREATE to fail after the first CREATE has succeeded.
	if _, err := db.Exec(`CREATE TABLE cast_settings(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, migrationFile{version: 2, name: "0002_cast.sql"}); err == nil {
		t.Fatal("expected failure")
	}
	if rawUserVersion(t, db) != 1 {
		t.Fatal("version advanced on failure")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='cast_cards'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial schema retained", n, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE content='history'`).Scan(&n); err != nil || n != 1 {
		t.Fatal("legacy data lost", err)
	}
}
