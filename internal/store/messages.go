package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Message is one node of a conversation's message tree. ParentID links a message to
// the one it answers or varies (nil for a root). Siblings are alternative variants
// (edits, rerolls); SelectedChildID remembers which child continues the chosen branch.
type Message struct {
	ID              int64
	ConversationID  int64
	ParentID        *int64
	Role            string
	Content         string
	Reasoning       string
	Status          string
	SpeakerJSON     string
	CreatedAt       time.Time
	SelectedChildID *int64
	// Origin records how a message came to exist: original, edit, reroll or fork.
	// OriginID names the message it was derived from (no foreign key: the source may
	// later be deleted without invalidating the derived message).
	Origin   string
	OriginID *int64
}

const messageColumns = `id, conversation_id, parent_id, role, content, reasoning, status, created_at, speaker_json, selected_child_id, origin, origin_id`

// CreateMessage inserts a new message and makes it the tip of the visible branch. A
// nil parentID appends below the current tip.
func (s *Store) CreateMessage(ctx context.Context, conversationID int64, parentID *int64, role, content, reasoning, status string) (*Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if parentID == nil {
		var leaf sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT active_leaf_id FROM conversations WHERE id=?`, conversationID).Scan(&leaf); err != nil {
			return nil, fmt.Errorf("create message: %w", err)
		}
		if leaf.Valid {
			parentID = &leaf.Int64
		}
	}
	id, err := insertMessage(ctx, tx, conversationID, parentID, role, content, reasoning, status, "{}", "original", nil)
	if err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}
	if err := setLeaf(ctx, tx, conversationID, parentID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMessage(ctx, id)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertMessage(ctx context.Context, tx execer, convID int64, parent *int64, role, content, reasoning, status, speaker, origin string, originID *int64) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, parent_id, role, content, reasoning, status, created_at, speaker_json, origin, origin_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		convID, parent, role, content, reasoning, status, formatTime(time.Now().UTC()), speaker, origin, originID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// setLeaf marks id as the chosen child of parent and the tip of the visible branch,
// and bumps the tree revision.
func setLeaf(ctx context.Context, tx execer, convID int64, parent *int64, id int64) error {
	if parent != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=? AND conversation_id=?`, id, *parent, convID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE conversations SET active_leaf_id=?, revision=revision+1, updated_at=? WHERE id=?`, id, formatTime(time.Now().UTC()), convID)
	return err
}

// GetMessage fetches one message by ID.
func (s *Store) GetMessage(ctx context.Context, id int64) (*Message, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = ?`, id)
	return scanMessage(row)
}

// ListMessages returns every message of every branch in a conversation, oldest first.
func (s *Store) ListMessages(ctx context.Context, conversationID int64) ([]*Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+messageColumns+` FROM messages WHERE conversation_id = ? ORDER BY id`, conversationID)
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
		selected  sql.NullInt64
		originID  sql.NullInt64
		createdAt string
	)
	if err := row.Scan(&m.ID, &m.ConversationID, &parentID, &m.Role, &m.Content, &m.Reasoning, &m.Status, &createdAt, &m.SpeakerJSON, &selected, &m.Origin, &originID); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan message: %w", err)
	}
	if parentID.Valid {
		m.ParentID = &parentID.Int64
	}
	if selected.Valid {
		m.SelectedChildID = &selected.Int64
	}
	if originID.Valid {
		m.OriginID = &originID.Int64
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = t
	return &m, nil
}
