package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Backup writes a consistent, timestamped copy of the database into
// dataDir/backups using VACUUM INTO, then empties the credential table
// in that copy (DR-016: credentials never leave the live database).
// It returns the backup file's path.
func (s *Store) Backup(ctx context.Context, dataDir string) (string, error) {
	backupDir := filepath.Join(dataDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("create backups directory: %w", err)
	}

	name := fmt.Sprintf("labbonet-%s.db", time.Now().UTC().Format("20060102T150405.000000000Z"))
	path := filepath.Join(backupDir, name)

	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("backup target %q already exists", path)
	}

	// VACUUM INTO takes the target path as a string literal, not a bind
	// parameter; the path is server-generated (never user input), but
	// quote it defensively anyway.
	quoted := strings.ReplaceAll(path, "'", "''")
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", quoted)); err != nil {
		return "", fmt.Errorf("vacuum into backup: %w", err)
	}

	if err := emptyCredentialsInBackup(path); err != nil {
		os.Remove(path)
		return "", err
	}

	return path, nil
}

// emptyCredentialsInBackup opens the freshly written backup file on its
// own connection and deletes every row from endpoint_credentials, so the
// backup never carries a usable credential.
func emptyCredentialsInBackup(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open backup for credential redaction: %w", err)
	}
	defer db.Close()

	if _, err := db.Exec("DELETE FROM endpoint_credentials"); err != nil {
		return fmt.Errorf("empty credentials in backup: %w", err)
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum backup after credential redaction: %w", err)
	}
	return nil
}
