package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Labyricorn/Labbonet/migrations"
)

// migrationFile is one parsed embedded migration.
type migrationFile struct {
	version int
	name    string
}

// sortedMigrations lists the embedded migrations in ascending version
// order, parsed from their "NNNN_description.sql" filenames.
func sortedMigrations() ([]migrationFile, error) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	files := make([]migrationFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration file %q does not follow the NNNN_description.sql naming convention", e.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration file %q has a non-numeric version prefix: %w", e.Name(), err)
		}
		files = append(files, migrationFile{version: version, name: e.Name()})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

// migrate brings db up to the latest embedded schema version, tracked by
// PRAGMA user_version. Each migration is applied inside its own
// transaction. A database newer than the latest embedded migration is
// refused without being modified.
func migrate(db *sql.DB, path string) error {
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	files, err := sortedMigrations()
	if err != nil {
		return err
	}

	latest := 0
	if len(files) > 0 {
		latest = files[len(files)-1].version
	}

	if current > latest {
		return fmt.Errorf("database schema version %d is newer than this binary supports (latest known migration is %d); refusing to open it to avoid data loss", current, latest)
	}
	// Back up an existing database before the first pending migration. The
	// normal credential-free backup path is reused; the live credentials remain.
	if current > 0 && current < latest {
		if _, err := (&Store{db: db}).Backup(context.Background(), filepath.Dir(path)); err != nil {
			return fmt.Errorf("pre-upgrade backup: %w", err)
		}
	}

	for _, f := range files {
		if f.version <= current {
			continue
		}
		if err := applyMigration(db, f); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(db *sql.DB, f migrationFile) error {
	raw, err := migrations.FS.ReadFile(f.name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", f.name, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", f.name, err)
	}
	defer tx.Rollback()

	for _, stmt := range splitStatements(string(raw)) {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("apply migration %s: %w", f.name, err)
		}
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", f.version)); err != nil {
		return fmt.Errorf("set schema version after %s: %w", f.name, err)
	}

	return tx.Commit()
}

// splitStatements splits a migration file into individual SQL statements
// on top-level semicolons. Migration files are authored by us and never
// contain a semicolon inside a string literal, so a plain split is
// sufficient; blank lines and whole-line "--" comments are dropped.
func splitStatements(sqlText string) []string {
	var kept []string
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept = append(kept, line)
	}
	joined := strings.Join(kept, "\n")

	var statements []string
	for _, part := range strings.Split(joined, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			statements = append(statements, part)
		}
	}
	return statements
}
