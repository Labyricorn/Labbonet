package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SetCredential stores (or replaces) the protected credential blob for an
// endpoint. The blob is opaque to the store; protecting and
// unprotecting it is internal/secrets' job (step 3).
func (s *Store) SetCredential(ctx context.Context, endpointID int64, protectedBlob []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO endpoint_credentials (endpoint_id, protected_blob, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(endpoint_id) DO UPDATE SET protected_blob = excluded.protected_blob, created_at = excluded.created_at`,
		endpointID, protectedBlob, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("set credential: %w", err)
	}
	return nil
}

// ClearCredential removes an endpoint's stored credential, if any.
func (s *Store) ClearCredential(ctx context.Context, endpointID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM endpoint_credentials WHERE endpoint_id = ?`, endpointID)
	if err != nil {
		return fmt.Errorf("clear credential: %w", err)
	}
	return nil
}

// HasCredential reports whether an endpoint has a stored credential,
// without exposing its value (API responses must only ever report
// "credential_set", never the blob).
func (s *Store) HasCredential(ctx context.Context, endpointID int64) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM endpoint_credentials WHERE endpoint_id = ?`, endpointID).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check credential: %w", err)
	}
	return true, nil
}

// CredentialBlob returns the raw protected blob for an endpoint. Callers
// outside internal/secrets must never log or serialize the result.
func (s *Store) CredentialBlob(ctx context.Context, endpointID int64) ([]byte, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx, `SELECT protected_blob FROM endpoint_credentials WHERE endpoint_id = ?`, endpointID).Scan(&blob)
	if err != nil {
		return nil, fmt.Errorf("read credential: %w", err)
	}
	return blob, nil
}
