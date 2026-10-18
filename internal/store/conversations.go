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
	// Revision increments on every change to the message tree (new turn, edit,
	// delete, branch switch) so a stale tab cannot overwrite another tab's work.
	Revision     int64
	ActiveLeafID *int64
	Pinned       bool
	FolderID     *int64
	Position     int
	Tags         []string
	// ForkedFrom* record provenance only; they are not foreign keys.
	ForkedFromConversationID *int64
	ForkedFromMessageID      *int64
}

const conversationColumns = `id, title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at, revision, active_leaf_id, pinned, folder_id, position, forked_from_conversation_id, forked_from_message_id`

// CreateConversation inserts a new conversation. EndpointID and ModelID
// may be nil (no model chosen yet).
func (s *Store) CreateConversation(ctx context.Context, title string, endpointID *int64, modelID *string, systemPrompt, paramsJSON string) (*Conversation, error) {
	now := formatTime(time.Now().UTC())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_cast(conversation_id,character_id,persona_id) SELECT ?,character_id,persona_id FROM cast_settings WHERE id=1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, id)
}

// GetConversation fetches one conversation by ID.
func (s *Store) GetConversation(ctx context.Context, id int64) (*Conversation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+conversationColumns+` FROM conversations WHERE id = ?`, id)
	c, err := scanConversation(row)
	if err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(ctx, id)
	if err != nil {
		return nil, err
	}
	c.Tags = tags[id]
	if c.Tags == nil {
		c.Tags = []string{}
	}
	return c, nil
}

// ListConversations returns every conversation: pinned first, then by manual
// position, then most recently updated first.
func (s *Store) ListConversations(ctx context.Context) ([]*Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+conversationColumns+` FROM conversations ORDER BY pinned DESC, position ASC, updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	var out []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := s.tagsFor(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, c := range out {
		c.Tags = tags[c.ID]
		if c.Tags == nil {
			c.Tags = []string{}
		}
	}
	return out, nil
}

// tagsFor loads tags for one conversation, or for all of them when id is 0.
func (s *Store) tagsFor(ctx context.Context, id int64) (map[int64][]string, error) {
	q, args := `SELECT conversation_id, tag FROM conversation_tags ORDER BY tag COLLATE NOCASE`, []any{}
	if id != 0 {
		q, args = `SELECT conversation_id, tag FROM conversation_tags WHERE conversation_id=? ORDER BY tag COLLATE NOCASE`, []any{id}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var cid int64
		var tag string
		if err := rows.Scan(&cid, &tag); err != nil {
			return nil, err
		}
		out[cid] = append(out[cid], tag)
	}
	return out, rows.Err()
}

// RenameConversation sets a conversation's title.
func (s *Store) RenameConversation(ctx context.Context, id int64, title string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET title = ?, updated_at = ? WHERE id = ?`,
		title, formatTime(time.Now().UTC()), id,
	)
	if err != nil {
		return fmt.Errorf("rename conversation: %w", err)
	}
	return nil
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
		leaf, folder         sql.NullInt64
		forkConv, forkMsg    sql.NullInt64
		pinned               int
		createdAt, updatedAt string
	)
	if err := row.Scan(&c.ID, &c.Title, &endpointID, &modelID, &c.SystemPrompt, &c.ParamsJSON, &createdAt, &updatedAt, &c.Revision, &leaf, &pinned, &folder, &c.Position, &forkConv, &forkMsg); err != nil {
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
	if leaf.Valid {
		c.ActiveLeafID = &leaf.Int64
	}
	if folder.Valid {
		c.FolderID = &folder.Int64
	}
	if forkConv.Valid {
		c.ForkedFromConversationID = &forkConv.Int64
	}
	if forkMsg.Valid {
		c.ForkedFromMessageID = &forkMsg.Int64
	}
	c.Pinned = pinned != 0
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
