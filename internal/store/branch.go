package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrBusy is returned when a tree edit is refused because a reply is still streaming.
var ErrBusy = errors.New("a reply is still generating; wait for it or stop it first")

// ErrInvalidMessage reports a rejected tree operation (bad id, role or content).
var ErrInvalidMessage = errors.New("invalid message operation")

const maxMessageBytes = 1 << 20

// PathMessage is a message on the visible branch together with the ids of every
// sibling variant (including itself), oldest first.
type PathMessage struct {
	*Message
	VariantIDs []int64
}

type tree struct {
	byID     map[int64]*Message
	children map[int64][]int64 // 0 = roots
	leaf     *int64
	revision int64
}

func (t *tree) siblings(m *Message) []int64 {
	if m.ParentID == nil {
		return t.children[0]
	}
	return t.children[*m.ParentID]
}

// descend follows each message's remembered child to the tip of its branch, falling
// back to the newest child if the remembered one is missing.
func (t *tree) descend(id int64) int64 {
	for guard := 0; guard < len(t.byID)+1; guard++ {
		m := t.byID[id]
		next := int64(0)
		if m.SelectedChildID != nil {
			if c := t.byID[*m.SelectedChildID]; c != nil && c.ParentID != nil && *c.ParentID == id {
				next = c.ID
			}
		}
		if next == 0 {
			if kids := t.children[id]; len(kids) > 0 {
				next = kids[len(kids)-1]
			}
		}
		if next == 0 {
			return id
		}
		id = next
	}
	return id
}

// pathTo returns the chain from a root to id, root first.
func (t *tree) pathTo(id int64) []*Message {
	var rev []*Message
	for guard := 0; guard <= len(t.byID); guard++ {
		m := t.byID[id]
		if m == nil {
			return nil
		}
		rev = append(rev, m)
		if m.ParentID == nil {
			break
		}
		id = *m.ParentID
	}
	out := make([]*Message, len(rev))
	for i, m := range rev {
		out[len(rev)-1-i] = m
	}
	return out
}

type dbq interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadTree(ctx context.Context, q dbq, convID int64) (*tree, error) {
	var leaf sql.NullInt64
	t := &tree{byID: map[int64]*Message{}, children: map[int64][]int64{}}
	if err := q.QueryRowContext(ctx, `SELECT active_leaf_id, revision FROM conversations WHERE id=?`, convID).Scan(&leaf, &t.revision); err != nil {
		return nil, err
	}
	if leaf.Valid {
		t.leaf = &leaf.Int64
	}
	rows, err := q.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE conversation_id=? ORDER BY id`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		t.byID[m.ID] = m
		key := int64(0)
		if m.ParentID != nil {
			key = *m.ParentID
		}
		t.children[key] = append(t.children[key], m.ID)
	}
	return t, rows.Err()
}

// ActivePath returns the visible branch of a conversation, root first, with variant
// information, and the conversation's tree revision. Only this path ever reaches a
// provider.
func (s *Store) ActivePath(ctx context.Context, convID int64) ([]PathMessage, int64, error) {
	t, err := loadTree(ctx, s.db, convID)
	if err != nil {
		return nil, 0, err
	}
	if t.leaf == nil || t.byID[*t.leaf] == nil {
		return []PathMessage{}, t.revision, nil
	}
	return t.annotate(t.pathTo(*t.leaf)), t.revision, nil
}

// PathTo returns the chain from the root to a message of the conversation.
func (s *Store) PathTo(ctx context.Context, convID, msgID int64) ([]PathMessage, int64, error) {
	t, err := loadTree(ctx, s.db, convID)
	if err != nil {
		return nil, 0, err
	}
	p := t.pathTo(msgID)
	if p == nil {
		return nil, 0, sql.ErrNoRows
	}
	return t.annotate(p), t.revision, nil
}

func (t *tree) annotate(path []*Message) []PathMessage {
	out := make([]PathMessage, len(path))
	for i, m := range path {
		out[i] = PathMessage{Message: m, VariantIDs: append([]int64(nil), t.siblings(m)...)}
	}
	return out
}

func hasStreaming(t *tree) bool {
	for _, m := range t.byID {
		if m.Status == "streaming" {
			return true
		}
	}
	return false
}

// lockTree starts a write transaction, verifies the caller's revision and refuses
// edits while a reply streams.
func (s *Store) lockTree(ctx context.Context, convID, expectedRevision int64) (*sql.Tx, *tree, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE conversations SET revision=revision WHERE id=? AND (?=0 OR revision=?)`, convID, expectedRevision, expectedRevision)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var exists int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM conversations WHERE id=?`, convID).Scan(&exists)
		tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, sql.ErrNoRows
		}
		return nil, nil, ErrConflict
	}
	t, err := loadTree(ctx, tx, convID)
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	if hasStreaming(t) {
		tx.Rollback()
		return nil, nil, ErrBusy
	}
	return tx, t, nil
}

// EditMessage stores an edited copy as a new sibling variant and makes it the visible
// one. The original stays intact (with its own continuation); the variant records the
// message it was edited from.
func (s *Store) EditMessage(ctx context.Context, convID, msgID int64, content string, expectedRevision int64) (*Message, error) {
	if strings.TrimSpace(content) == "" || len(content) > maxMessageBytes || !utf8.ValidString(content) {
		return nil, ErrInvalidMessage
	}
	tx, t, err := s.lockTree(ctx, convID, expectedRevision)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	src := t.byID[msgID]
	if src == nil {
		return nil, sql.ErrNoRows
	}
	id, err := insertMessage(ctx, tx, convID, src.ParentID, src.Role, content, "", "complete", src.SpeakerJSON, "edit", &src.ID)
	if err != nil {
		return nil, err
	}
	if err := setLeaf(ctx, tx, convID, src.ParentID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMessage(ctx, id)
}

// DeleteMessage removes a message and everything below it (its continuation in that
// variant). Sibling variants are untouched, and the visible branch moves to the
// nearest remaining sibling or to the parent.
func (s *Store) DeleteMessage(ctx context.Context, convID, msgID int64, expectedRevision int64) error {
	tx, t, err := s.lockTree(ctx, convID, expectedRevision)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target := t.byID[msgID]
	if target == nil {
		return sql.ErrNoRows
	}
	doomed := map[int64]bool{}
	var walk func(id int64)
	walk = func(id int64) {
		doomed[id] = true
		for _, c := range t.children[id] {
			walk(c)
		}
	}
	walk(msgID)
	// One statement, so the self-referencing foreign key is checked after the whole
	// subtree is gone.
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id=? AND id IN (WITH RECURSIVE sub(id) AS (SELECT ? UNION ALL SELECT m.id FROM messages m JOIN sub ON m.parent_id=sub.id) SELECT id FROM sub)`, convID, msgID); err != nil {
		return err
	}
	var remaining []int64
	for _, id := range t.siblings(target) {
		if id != msgID {
			remaining = append(remaining, id)
		}
	}
	// Prefer the previous sibling, then the next one.
	var pick int64
	for _, id := range remaining {
		if id < msgID {
			pick = id
		}
	}
	if pick == 0 && len(remaining) > 0 {
		pick = remaining[0]
	}
	for id := range doomed {
		delete(t.byID, id)
	}
	if target.ParentID != nil {
		var sel any
		if pick != 0 {
			sel = pick
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=?`, sel, *target.ParentID); err != nil {
			return err
		}
		if p := t.byID[*target.ParentID]; p != nil {
			p.SelectedChildID = nil
			if pick != 0 {
				p.SelectedChildID = &pick
			}
		}
	}
	if t.leaf != nil && doomed[*t.leaf] {
		var newLeaf any
		switch {
		case pick != 0:
			newLeaf = t.descend(pick)
		case target.ParentID != nil:
			newLeaf = *target.ParentID
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET active_leaf_id=? WHERE id=?`, newLeaf, convID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET revision=revision+1, updated_at=? WHERE id=?`, formatTime(time.Now().UTC()), convID); err != nil {
		return err
	}
	return tx.Commit()
}

// ActivateMessage makes a message the visible variant at its position; the branch
// continues through that variant's own remembered continuation.
func (s *Store) ActivateMessage(ctx context.Context, convID, msgID int64, expectedRevision int64) error {
	tx, t, err := s.lockTree(ctx, convID, expectedRevision)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	m := t.byID[msgID]
	if m == nil {
		return sql.ErrNoRows
	}
	// Re-select every ancestor link so the whole route to this variant is visible.
	for _, anc := range t.pathTo(msgID) {
		if anc.ParentID == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=?`, anc.ID, *anc.ParentID); err != nil {
			return err
		}
		t.byID[*anc.ParentID].SelectedChildID = &anc.ID
	}
	leaf := t.descend(msgID)
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET active_leaf_id=?, revision=revision+1, updated_at=? WHERE id=?`, leaf, formatTime(time.Now().UTC()), convID); err != nil {
		return err
	}
	return tx.Commit()
}

// ForkConversation copies the chain from the root to msgID into a new conversation
// (same model, system prompt, cast, folder and tags). Cloned messages record the
// message they were copied from; generation provenance stays with the source.
func (s *Store) ForkConversation(ctx context.Context, convID, msgID int64, title string) (*Conversation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET revision=revision WHERE id=?`, convID); err != nil {
		return nil, err
	}
	t, err := loadTree(ctx, tx, convID)
	if err != nil {
		return nil, err
	}
	path := t.pathTo(msgID)
	if len(path) == 0 {
		return nil, sql.ErrNoRows
	}
	now := formatTime(time.Now().UTC())
	res, err := tx.ExecContext(ctx, `INSERT INTO conversations(title,endpoint_id,model_id,system_prompt,params_json,created_at,updated_at,folder_id,forked_from_conversation_id,forked_from_message_id)
		SELECT ?,endpoint_id,model_id,system_prompt,params_json,?,?,folder_id,id,? FROM conversations WHERE id=?`, title, now, now, msgID, convID)
	if err != nil {
		return nil, err
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_cast(conversation_id,character_id,persona_id,greeting) SELECT ?,character_id,persona_id,greeting FROM conversation_cast WHERE conversation_id=?`, newID, convID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_tags(conversation_id,tag) SELECT ?,tag FROM conversation_tags WHERE conversation_id=?`, newID, convID); err != nil {
		return nil, err
	}
	var parent *int64
	var last int64
	for _, m := range path {
		status := m.Status
		if status == "streaming" {
			status = "interrupted"
		}
		src := m.ID
		id, err := insertMessage(ctx, tx, newID, parent, m.Role, m.Content, m.Reasoning, status, m.SpeakerJSON, "fork", &src)
		if err != nil {
			return nil, err
		}
		if parent != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=?`, id, *parent); err != nil {
				return nil, err
			}
		}
		parent, last = &id, id
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET active_leaf_id=? WHERE id=?`, last, newID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("fork conversation: %w", err)
	}
	return s.GetConversation(ctx, newID)
}
