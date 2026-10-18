// Package storetest gives each test a fresh, migrated Labbonet database
// in a temp data directory, mirroring internal/server/servertest.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/store"
)

// New opens a fresh migrated database in a temp directory and registers
// cleanup to close it.
func New(t *testing.T) *store.Store {
	t.Helper()
	st, _ := NewWithDir(t)
	return st
}

// NewWithDir is New, but also returns the temp data directory the
// database lives in (for tests that exercise backup, which writes into
// dataDir/backups).
func NewWithDir(t *testing.T) (*store.Store, string) {
	t.Helper()

	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "labbonet.db")

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	return st, dataDir
}
