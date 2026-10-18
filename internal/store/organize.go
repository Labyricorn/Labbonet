package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidOrganization reports a rejected folder, tag or ordering request.
var ErrInvalidOrganization = errors.New("invalid organization request")

const (
	maxTags       = 16
	maxTagRunes   = 32
	maxFolderName = 60
	maxFolders    = 200
)

// Folder groups conversations in the sidebar.
type Folder struct {
	ID       int64
	Name     string
	Position int
}

// NormalizeTag trims and validates one tag. Tags are case-insensitive for equality but
// keep their first-seen case for display.
func NormalizeTag(raw string) (string, error) {
	tag := strings.Join(strings.Fields(raw), " ")
	tag = strings.TrimLeft(tag, "#")
	if tag == "" || utf8.RuneCountInString(tag) > maxTagRunes || !utf8.ValidString(tag) {
		return "", ErrInvalidOrganization
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return "", ErrInvalidOrganization
		}
	}
	return tag, nil
}

// NormalizeTags validates a tag list and removes case-insensitive duplicates.
func NormalizeTags(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, r := range raw {
		tag, err := NormalizeTag(r)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, ErrInvalidOrganization
	}
	return out, nil
}

func normalizeFolderName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxFolderName || !utf8.ValidString(name) {
		return "", ErrInvalidOrganization
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalidOrganization
		}
	}
	return name, nil
}

// Organization is a partial update of a conversation's sidebar placement. Nil fields
// are left unchanged; ClearFolder moves the conversation out of any folder.
type Organization struct {
	Pinned      *bool
	FolderID    *int64
	ClearFolder bool
	Tags        *[]string
}

// SetOrganization applies pin, folder and tag changes in one transaction.
func (s *Store) SetOrganization(ctx context.Context, id int64, o Organization) error {
	var tags []string
	if o.Tags != nil {
		var err error
		if tags, err = NormalizeTags(*o.Tags); err != nil {
			return err
		}
	}
	if o.FolderID != nil && o.ClearFolder {
		return ErrInvalidOrganization
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE conversations SET id=id WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	if o.Pinned != nil {
		v := 0
		if *o.Pinned {
			v = 1
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET pinned=? WHERE id=?`, v, id); err != nil {
			return err
		}
	}
	if o.FolderID != nil {
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM folders WHERE id=?`, *o.FolderID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInvalidOrganization
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET folder_id=? WHERE id=?`, *o.FolderID, id); err != nil {
			return err
		}
	}
	if o.ClearFolder {
		if _, err := tx.ExecContext(ctx, `UPDATE conversations SET folder_id=NULL WHERE id=?`, id); err != nil {
			return err
		}
	}
	if o.Tags != nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_tags WHERE conversation_id=?`, id); err != nil {
			return err
		}
		for _, tag := range tags {
			if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_tags(conversation_id,tag) VALUES(?,?)`, id, tag); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ReorderConversations stores a manual order. ids must list every conversation
// exactly once; otherwise another tab changed the list and ErrConflict is returned.
func (s *Store) ReorderConversations(ctx context.Context, ids []int64) error {
	if len(ids) > 100000 {
		return ErrInvalidOrganization
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET position=position WHERE id=(SELECT MIN(id) FROM conversations)`); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations`).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return ErrConflict
	}
	seen := map[int64]bool{}
	for i, id := range ids {
		if seen[id] {
			return ErrInvalidOrganization
		}
		seen[id] = true
		res, err := tx.ExecContext(ctx, `UPDATE conversations SET position=? WHERE id=?`, i+1, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
	}
	return tx.Commit()
}

// ListFolders returns folders in display order.
func (s *Store) ListFolders(ctx context.Context) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,position FROM folders ORDER BY position, id`)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Position); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ReorderFolders applies a complete permutation only when the caller's displayed
// order still matches storage, so a stale tab cannot overwrite another reorder.
func (s *Store) ReorderFolders(ctx context.Context, expected, ids []int64) error {
	if len(ids) > maxFolders || len(ids) != len(expected) {
		return ErrInvalidOrganization
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM folders ORDER BY position,id`)
	if err != nil {
		return err
	}
	var current []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		current = append(current, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(current) != len(expected) {
		return ErrConflict
	}
	seen := map[int64]bool{}
	for i, id := range current {
		if id != expected[i] {
			return ErrConflict
		}
		seen[id] = true
	}
	for _, id := range ids {
		if !seen[id] {
			return ErrInvalidOrganization
		}
		delete(seen, id)
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE folders SET position=? WHERE id=?`, i, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateFolder adds a folder at the end. Names are unique ignoring case.
func (s *Store) CreateFolder(ctx context.Context, name string) (*Folder, error) {
	name, err := normalizeFolderName(name)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count, next int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(position)+1,0) FROM folders`).Scan(&count, &next); err != nil {
		return nil, err
	}
	if count >= maxFolders {
		return nil, ErrInvalidOrganization
	}
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM folders WHERE name=? COLLATE NOCASE`, name).Scan(&one); err == nil {
		return nil, ErrInvalidOrganization
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO folders(name,position,created_at) VALUES(?,?,?)`, name, next, formatTime(time.Now().UTC()))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Folder{ID: id, Name: name, Position: next}, nil
}

// RenameFolder renames a folder, keeping names unique ignoring case.
func (s *Store) RenameFolder(ctx context.Context, id int64, name string) error {
	name, err := normalizeFolderName(name)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM folders WHERE name=? COLLATE NOCASE AND id<>?`, name, id).Scan(&one); err == nil {
		return ErrInvalidOrganization
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE folders SET name=? WHERE id=?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// DeleteFolder removes a folder; its conversations return to the unfiled list.
func (s *Store) DeleteFolder(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM folders WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// SearchHit says where a search term matched in one conversation.
type SearchHit struct {
	ConversationID int64
	Where          string // title, tag or message
	Snippet        string
}

func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(term) + "%"
}

// SearchConversations finds conversations whose title, tags or any message of any
// branch contains term (case-insensitive). Results keep list order.
func (s *Store) SearchConversations(ctx context.Context, term string) ([]SearchHit, error) {
	term = strings.TrimSpace(term)
	if utf8.RuneCountInString(term) < 2 || utf8.RuneCountInString(term) > 200 {
		return nil, ErrInvalidOrganization
	}
	pat := likePattern(term)
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id,
  CASE WHEN c.title LIKE ?1 ESCAPE '\' THEN 'title'
       WHEN EXISTS(SELECT 1 FROM conversation_tags t WHERE t.conversation_id=c.id AND t.tag LIKE ?1 ESCAPE '\') THEN 'tag'
       ELSE 'message' END,
  COALESCE((SELECT m.content FROM messages m WHERE m.conversation_id=c.id AND m.content LIKE ?1 ESCAPE '\' ORDER BY m.id LIMIT 1), '')
FROM conversations c
WHERE c.title LIKE ?1 ESCAPE '\'
   OR EXISTS(SELECT 1 FROM conversation_tags t WHERE t.conversation_id=c.id AND t.tag LIKE ?1 ESCAPE '\')
   OR EXISTS(SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.content LIKE ?1 ESCAPE '\')
ORDER BY c.pinned DESC, c.position ASC, c.updated_at DESC, c.id DESC
LIMIT 500`, pat)
	if err != nil {
		return nil, fmt.Errorf("search conversations: %w", err)
	}
	defer rows.Close()
	out := []SearchHit{}
	lower := strings.ToLower(term)
	for rows.Next() {
		var h SearchHit
		var content string
		if err := rows.Scan(&h.ConversationID, &h.Where, &content); err != nil {
			return nil, err
		}
		if h.Where == "message" || h.Where == "title" {
			h.Snippet = snippet(content, lower)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func snippet(content, lowerTerm string) string {
	if content == "" {
		return ""
	}
	runes := []rune(content)
	lowered := strings.ToLower(content)
	idx := strings.Index(lowered, lowerTerm)
	start := 0
	if idx > 0 {
		start = utf8.RuneCountInString(lowered[:idx]) - 30
		if start < 0 {
			start = 0
		}
	}
	if start > len(runes) {
		start = len(runes)
	}
	end := start + 100
	if end > len(runes) {
		end = len(runes)
	}
	out := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(runes) {
		out += "…"
	}
	return out
}
