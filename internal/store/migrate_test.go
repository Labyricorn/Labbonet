package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// latestKnownVersion is the current schema version (PHASE1-ACCEPTANCE.md
// A-B5 to A-B7). Update when a new migration is added.
const latestKnownVersion = 5

// A-B5: a fresh start creates the schema, and a restart on the same file
// keeps the data and lands on the same user_version.
func TestOpen_FreshStartCreatesSchemaAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labbonet.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := userVersion(t, path); got != latestKnownVersion {
		t.Fatalf("user_version = %d, want %d", got, latestKnownVersion)
	}

	ep, err := st.CreateEndpoint(t.Context(), "Strata", "openai", "http://127.0.0.1:8080", "local", 1)
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	st.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	got, err := st2.GetEndpoint(t.Context(), ep.ID)
	if err != nil {
		t.Fatalf("get endpoint after restart: %v", err)
	}
	if got.Name != "Strata" {
		t.Fatalf("endpoint name after restart = %q, want %q", got.Name, "Strata")
	}
	if v := userVersion(t, path); v != latestKnownVersion {
		t.Fatalf("user_version after restart = %d, want %d", v, latestKnownVersion)
	}
}

// A-B6: migrating an empty version-zero database applies all embedded
// migrations in order. The cast upgrade test covers populated version one.
func TestMigrate_FromEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labbonet.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if v := rawUserVersion(t, db); v != 0 {
		t.Fatalf("new database user_version = %d, want 0", v)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	if v := userVersion(t, path); v != latestKnownVersion {
		t.Fatalf("user_version after migrate = %d, want %d", v, latestKnownVersion)
	}

	// Re-opening an already-migrated database is a no-op that leaves data
	// intact without applying the same migration twice.
	if _, err := st.CreateEndpoint(t.Context(), "LM Studio", "openai", "http://10.138.11.11:1234", "lan", 1); err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	st.Close()

	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen already-migrated database: %v", err)
	}
	defer st2.Close()

	eps, err := st2.ListEndpoints(t.Context())
	if err != nil {
		t.Fatalf("list endpoints: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("len(endpoints) = %d, want 1 (re-migration must not touch data)", len(eps))
	}
}

// A-B7: a database newer than the binary knows about is refused, and the
// file is left byte-for-byte unmodified.
func TestOpen_RefusesNewerDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labbonet.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	st.Close()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	db.Close()

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatalf("Open on a database newer than this binary succeeded, want an error")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("Open modified a database it refused to open")
	}
}

func userVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw for user_version: %v", err)
	}
	defer db.Close()
	return rawUserVersion(t, db)
}

func rawUserVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	return v
}
