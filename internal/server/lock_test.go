package server_test

import (
	"testing"

	"github.com/Labyricorn/Labbonet/internal/server"
)

// A-B3: a second instance pointed at the same data directory is refused
// by the lock file.
func TestSecondLockOnSameDataDirFails(t *testing.T) {
	dataDir := t.TempDir()

	first, err := server.AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}

	if _, err := server.AcquireDataDirLock(dataDir); err == nil {
		t.Fatal("expected second lock on the same data directory to fail")
	}

	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	second, err := server.AcquireDataDirLock(dataDir)
	if err != nil {
		t.Fatalf("lock should succeed again after release: %v", err)
	}
	second.Release()
}
