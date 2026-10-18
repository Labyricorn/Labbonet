package store

import (
	"context"
	"fmt"
	"time"
)

// Model is one discovered model on one endpoint. Identity is always the
// pair (EndpointID, ModelID) (ARCHITECTURE.md section 5).
type Model struct {
	EndpointID       int64
	ModelID          string
	Type             string
	State            string
	ContextLength    int
	CapabilitiesJSON string
	SeenAt           time.Time
}

// UpsertModel records (or refreshes) one model seen on an endpoint.
func (s *Store) UpsertModel(ctx context.Context, m Model) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO models (endpoint_id, model_id, type, state, context_length, capabilities_json, seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(endpoint_id, model_id) DO UPDATE SET
		   type = excluded.type,
		   state = excluded.state,
		   context_length = excluded.context_length,
		   capabilities_json = excluded.capabilities_json,
		   seen_at = excluded.seen_at`,
		m.EndpointID, m.ModelID, m.Type, m.State, m.ContextLength, m.CapabilitiesJSON, formatTime(m.SeenAt),
	)
	if err != nil {
		return fmt.Errorf("upsert model: %w", err)
	}
	return nil
}

// ListModels returns every model seen on an endpoint, most recently seen
// first.
func (s *Store) ListModels(ctx context.Context, endpointID int64) ([]*Model, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT endpoint_id, model_id, type, state, context_length, capabilities_json, seen_at
		 FROM models WHERE endpoint_id = ? ORDER BY seen_at DESC`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var out []*Model
	for rows.Next() {
		var (
			m      Model
			seenAt string
		)
		if err := rows.Scan(&m.EndpointID, &m.ModelID, &m.Type, &m.State, &m.ContextLength, &m.CapabilitiesJSON, &seenAt); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		t, err := parseTime(seenAt)
		if err != nil {
			return nil, err
		}
		m.SeenAt = t
		out = append(out, &m)
	}
	return out, rows.Err()
}
