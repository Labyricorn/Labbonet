package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Message is one turn in a conversation. ParentID is kept for later
// branching (not used in Phase 1).
type Message struct {
	ID             int64
	ConversationID int64
	ParentID       *int64
	Role           string
	Content        string
	Reasoning      string
	Status         string
	CreatedAt      time.Time
}

// CreateMessage inserts a new message.
func (s *Store) CreateMessage(ctx context.Context, conversationID int64, parentID *int64, role, content, reasoning, status string) (*Message, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, parent_id, role, content, reasoning, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conversationID, parentID, role, content, reasoning, status, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}
	return s.GetMessage(ctx, id)
}

// GetMessage fetches one message by ID.
func (s *Store) GetMessage(ctx context.Context, id int64) (*Message, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, conversation_id, parent_id, role, content, reasoning, status, created_at
		 FROM messages WHERE id = ?`, id)
	return scanMessage(row)
}

// ListMessages returns every message in a conversation, oldest first.
func (s *Store) ListMessages(ctx context.Context, conversationID int64) ([]*Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, parent_id, role, content, reasoning, status, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()

	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateMessageContent sets a message's content, reasoning and status
// (used to flush streamed content and to record the final status:
// complete, cancelled, interrupted or error).
func (s *Store) UpdateMessageContent(ctx context.Context, id int64, content, reasoning, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE messages SET content = ?, reasoning = ?, status = ? WHERE id = ?`,
		content, reasoning, status, id,
	)
	if err != nil {
		return fmt.Errorf("update message content: %w", err)
	}
	return nil
}

func scanMessage(row rowScanner) (*Message, error) {
	var (
		m         Message
		parentID  sql.NullInt64
		createdAt string
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &parentID, &m.Role, &m.Content, &m.Reasoning, &m.Status, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan message: %w", err)
	}
	if parentID.Valid {
		m.ParentID = &parentID.Int64
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = t
	return &m, nil
}
