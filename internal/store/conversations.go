package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Conversation is a chat thread (ARCHITECTURE.md section 5).
type Conversation struct {
	ID           int64
	Title        string
	EndpointID   *int64
	ModelID      *string
	SystemPrompt string
	ParamsJSON   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateConversation inserts a new conversation. EndpointID and ModelID
// may be nil (no model chosen yet).
func (s *Store) CreateConversation(ctx context.Context, title string, endpointID *int64, modelID *string, systemPrompt, paramsJSON string) (*Conversation, error) {
	now := formatTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		title, endpointID, modelID, systemPrompt, paramsJSON, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return s.GetConversation(ctx, id)
}

// GetConversation fetches one conversation by ID.
func (s *Store) GetConversation(ctx context.Context, id int64) (*Conversation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at
		 FROM conversations WHERE id = ?`, id)
	return scanConversation(row)
}

// ListConversations returns every conversation, most recently updated
// first.
func (s *Store) ListConversations(ctx context.Context) ([]*Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at
		 FROM conversations ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var out []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetConversationModel changes a conversation's current endpoint/model
// (a model switch keeps all prior messages, PHASE1-PLAN.md step 5).
func (s *Store) SetConversationModel(ctx context.Context, id int64, endpointID *int64, modelID *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET endpoint_id = ?, model_id = ?, updated_at = ? WHERE id = ?`,
		endpointID, modelID, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("set conversation model: %w", err)
	}
	return nil
}

// DeleteConversation removes a conversation and (via ON DELETE CASCADE)
// its messages and generations.
func (s *Store) DeleteConversation(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	return nil
}

func scanConversation(row rowScanner) (*Conversation, error) {
	var (
		c                    Conversation
		endpointID           sql.NullInt64
		modelID              sql.NullString
		createdAt, updatedAt string
	)
	if err := row.Scan(&c.ID, &c.Title, &endpointID, &modelID, &c.SystemPrompt, &c.ParamsJSON, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	if endpointID.Valid {
		c.EndpointID = &endpointID.Int64
	}
	if modelID.Valid {
		c.ModelID = &modelID.String
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	c.CreatedAt = t
	t, err = parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	c.UpdatedAt = t
	return &c, nil
}
