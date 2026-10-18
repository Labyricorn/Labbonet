// Package store is the authoritative SQLite database: opening with the
// Phase 1 pragmas, versioned migrations, typed queries per table, and
// backup (DR-006).
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps the database connection used by the rest of the backend.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at path, sets the
// Phase 1 pragmas (WAL journal, foreign keys on, a busy timeout so
// concurrent access waits instead of failing immediately), and applies
// any pending migrations. It refuses a database whose schema version is
// newer than this binary knows about, leaving the file untouched.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// A single physical connection: modernc.org/sqlite (like other SQLite
	// drivers) serializes access per connection, and WAL mode plus the
	// busy timeout below are set per-connection pragmas.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", pragma, err)
		}
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}
