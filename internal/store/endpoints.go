package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Endpoint is a registered provider endpoint (ARCHITECTURE.md section 5).
type Endpoint struct {
	ID               int64
	Name             string
	ProviderType     string
	BaseURL          string
	PrivacyClass     string
	Enabled          bool
	ConcurrencyLimit int
	DetectedService  string
	CreatedAt        time.Time
}

// CreateEndpoint inserts a new endpoint and returns it with its assigned
// ID and creation time.
func (s *Store) CreateEndpoint(ctx context.Context, name, providerType, baseURL, privacyClass string, concurrencyLimit int) (*Endpoint, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO endpoints (name, provider_type, base_url, privacy_class, enabled, concurrency_limit, detected_service, created_at)
		 VALUES (?, ?, ?, ?, 1, ?, '', ?)`,
		name, providerType, baseURL, privacyClass, concurrencyLimit, formatTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}
	return s.GetEndpoint(ctx, id)
}

// GetEndpoint fetches one endpoint by ID.
func (s *Store) GetEndpoint(ctx context.Context, id int64) (*Endpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, provider_type, base_url, privacy_class, enabled, concurrency_limit, detected_service, created_at
		 FROM endpoints WHERE id = ?`, id)
	return scanEndpoint(row)
}

// ListEndpoints returns every registered endpoint, oldest first.
func (s *Store) ListEndpoints(ctx context.Context) ([]*Endpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, provider_type, base_url, privacy_class, enabled, concurrency_limit, detected_service, created_at
		 FROM endpoints ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list endpoints: %w", err)
	}
	defer rows.Close()

	var out []*Endpoint
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteEndpoint removes an endpoint and (via ON DELETE CASCADE) its
// credential and model inventory.
func (s *Store) DeleteEndpoint(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete endpoint: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEndpoint(row rowScanner) (*Endpoint, error) {
	var (
		e         Endpoint
		enabled   int
		createdAt string
	)
	if err := row.Scan(&e.ID, &e.Name, &e.ProviderType, &e.BaseURL, &e.PrivacyClass, &enabled, &e.ConcurrencyLimit, &e.DetectedService, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan endpoint: %w", err)
	}
	e.Enabled = enabled != 0
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	e.CreatedAt = t
	return &e, nil
}
