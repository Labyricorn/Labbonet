package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Generation records the provenance of one assistant reply: which
// endpoint and model served it, timing, usage and how it ended
// (ARCHITECTURE.md section 4.2 and 5).
type Generation struct {
	ID             int64
	MessageID      int64
	EndpointID     *int64
	ModelRequested string
	ModelReported  string
	ParamsJSON     string
	StartedAt      time.Time
	FirstTokenAt   *time.Time
	FinishedAt     *time.Time
	FinishReason   string
	UsageJSON      string
	TimingsJSON    string
	ErrorKind      string
}

// CreateGeneration inserts a new generation row at the start of a
// request, before any response has arrived.
func (s *Store) CreateGeneration(ctx context.Context, messageID int64, endpointID *int64, modelRequested, paramsJSON string) (*Generation, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO generations (message_id, endpoint_id, model_requested, model_reported, params_json, started_at, finish_reason, usage_json, timings_json, error_kind)
		 VALUES (?, ?, ?, '', ?, ?, '', '{}', '{}', '')`,
		messageID, endpointID, modelRequested, paramsJSON, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return nil, fmt.Errorf("create generation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create generation: %w", err)
	}
	return s.GetGeneration(ctx, id)
}

// GetGeneration fetches one generation by ID.
func (s *Store) GetGeneration(ctx context.Context, id int64) (*Generation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, message_id, endpoint_id, model_requested, model_reported, params_json, started_at, first_token_at, finished_at, finish_reason, usage_json, timings_json, error_kind
		 FROM generations WHERE id = ?`, id)
	return scanGeneration(row)
}

// ListGenerationsForMessage returns every generation recorded for a
// message (normally one, but kept as a list for later retry support).
func (s *Store) ListGenerationsForMessage(ctx context.Context, messageID int64) ([]*Generation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, endpoint_id, model_requested, model_reported, params_json, started_at, first_token_at, finished_at, finish_reason, usage_json, timings_json, error_kind
		 FROM generations WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, fmt.Errorf("list generations: %w", err)
	}
	defer rows.Close()

	var out []*Generation
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// RecordFirstToken sets the first-token timestamp and the model actually
// reported by the provider, the first time either becomes known.
func (s *Store) RecordFirstToken(ctx context.Context, id int64, modelReported string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE generations SET model_reported = ?, first_token_at = ? WHERE id = ? AND first_token_at IS NULL`,
		modelReported, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("record first token: %w", err)
	}
	return nil
}

// SetModelReported records the model ID the provider actually
// reported, independently of first-token timing: PROVIDER-SPEC.md
// section 1 requires the adapter to record "the model ID the server
// reports in its response, not the one requested", which is only known
// once a chunk carrying it (normally the final one) arrives, separate
// from when the first token arrived.
func (s *Store) SetModelReported(ctx context.Context, id int64, modelReported string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE generations SET model_reported = ? WHERE id = ?`, modelReported, id)
	if err != nil {
		return fmt.Errorf("set model reported: %w", err)
	}
	return nil
}

// ListRecentGenerations returns the most recently started generations,
// most recent first, for the diagnostics panel (ARCHITECTURE.md
// section 4.1). It never touches the messages table, so it can never
// include conversation content (SECURITY-MODEL.md section 7).
func (s *Store) ListRecentGenerations(ctx context.Context, limit int) ([]*Generation, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, message_id, endpoint_id, model_requested, model_reported, params_json, started_at, first_token_at, finished_at, finish_reason, usage_json, timings_json, error_kind
		 FROM generations ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent generations: %w", err)
	}
	defer rows.Close()

	var out []*Generation
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// FinishGeneration records the terminal state of a generation: finish
// reason, usage/timings JSON and the error kind (empty on success).
func (s *Store) FinishGeneration(ctx context.Context, id int64, finishReason, usageJSON, timingsJSON, errorKind string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE generations SET finished_at = ?, finish_reason = ?, usage_json = ?, timings_json = ?, error_kind = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), finishReason, usageJSON, timingsJSON, errorKind, id,
	)
	if err != nil {
		return fmt.Errorf("finish generation: %w", err)
	}
	return nil
}

func scanGeneration(row rowScanner) (*Generation, error) {
	var (
		g                        Generation
		endpointID               sql.NullInt64
		startedAt                string
		firstTokenAt, finishedAt sql.NullString
	)
	if err := row.Scan(&g.ID, &g.MessageID, &endpointID, &g.ModelRequested, &g.ModelReported, &g.ParamsJSON, &startedAt, &firstTokenAt, &finishedAt, &g.FinishReason, &g.UsageJSON, &g.TimingsJSON, &g.ErrorKind); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan generation: %w", err)
	}
	if endpointID.Valid {
		g.EndpointID = &endpointID.Int64
	}
	t, err := parseTime(startedAt)
	if err != nil {
		return nil, err
	}
	g.StartedAt = t
	if firstTokenAt.Valid {
		t, err := parseTime(firstTokenAt.String)
		if err != nil {
			return nil, err
		}
		g.FirstTokenAt = &t
	}
	if finishedAt.Valid {
		t, err := parseTime(finishedAt.String)
		if err != nil {
			return nil, err
		}
		g.FinishedAt = &t
	}
	return &g, nil
}
