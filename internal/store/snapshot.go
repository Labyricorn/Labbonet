package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
	"github.com/Labyricorn/Labbonet/internal/interchange"
)

var ErrInvalidImport = errors.New("invalid import")

// ExportSnapshot builds a credential-free snapshot. scope "conversation" includes the
// thread plus the cards it references; "cards" exports the library only.
func (s *Store) ExportSnapshot(ctx context.Context, scope string, conversationID int64) (*interchange.Snapshot, error) {
	if scope != "full" && scope != "cards" && scope != "conversation" {
		return nil, ErrInvalidImport
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	snap := &interchange.Snapshot{Format: interchange.SnapshotFormat, Version: interchange.SnapshotVersion, Scope: scope,
		ExportedAt: formatTime(time.Now().UTC()), Cards: []interchange.SnapCard{}, Conversations: []interchange.SnapConversation{}}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,revision,position,data_json FROM cast_cards ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	cards := map[int64]cast.Card{}
	var order []int64
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		cards[c.ID] = *c
		order = append(order, c.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	used := map[int64]bool{}
	key := func(id *int64) string {
		if id == nil {
			return ""
		}
		if _, ok := cards[*id]; !ok {
			return ""
		}
		used[*id] = true
		return "c" + strconv.FormatInt(*id, 10)
	}
	if scope != "cards" {
		q := `SELECT id,title,system_prompt,created_at FROM conversations`
		args := []any{}
		if scope == "conversation" {
			q += ` WHERE id=?`
			args = append(args, conversationID)
		}
		crows, err := tx.QueryContext(ctx, q+` ORDER BY id`, args...)
		if err != nil {
			return nil, err
		}
		type head struct {
			id    int64
			convo interchange.SnapConversation
		}
		var heads []head
		for crows.Next() {
			var h head
			var created string
			if err := crows.Scan(&h.id, &h.convo.Title, &h.convo.SystemPrompt, &created); err != nil {
				crows.Close()
				return nil, err
			}
			h.convo.Key = "t" + strconv.FormatInt(h.id, 10)
			h.convo.CreatedAt = created
			heads = append(heads, h)
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return nil, err
		}
		if scope == "conversation" && len(heads) == 0 {
			return nil, sql.ErrNoRows
		}
		for _, h := range heads {
			var ch, pe *int64
			var greeting int
			err := tx.QueryRowContext(ctx, `SELECT character_id,persona_id,greeting FROM conversation_cast WHERE conversation_id=?`, h.id).Scan(&ch, &pe, &greeting)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			h.convo.Character, h.convo.Persona = key(ch), key(pe)
			if h.convo.Character != "" {
				h.convo.Greeting = greeting
			}
			mrows, err := tx.QueryContext(ctx, `SELECT role,content,reasoning,status,created_at,speaker_json FROM messages WHERE conversation_id=? ORDER BY id`, h.id)
			if err != nil {
				return nil, err
			}
			h.convo.Messages = []interchange.SnapMessage{}
			for mrows.Next() {
				var m interchange.SnapMessage
				var speaker string
				if err := mrows.Scan(&m.Role, &m.Content, &m.Reasoning, &m.Status, &m.CreatedAt, &speaker); err != nil {
					mrows.Close()
					return nil, err
				}
				if m.Status == "streaming" {
					m.Status = "interrupted"
				}
				var id cast.Identity
				if json.Unmarshal([]byte(speaker), &id) == nil && id != (cast.Identity{}) {
					id.ID, id.Revision = 0, 0
					m.Speaker = &id
				}
				h.convo.Messages = append(h.convo.Messages, m)
			}
			mrows.Close()
			if err := mrows.Err(); err != nil {
				return nil, err
			}
			snap.Conversations = append(snap.Conversations, h.convo)
		}
	}
	if scope == "full" {
		var ch, pe *int64
		if err := tx.QueryRowContext(ctx, `SELECT character_id,persona_id FROM cast_settings WHERE id=1`).Scan(&ch, &pe); err != nil {
			return nil, err
		}
		if d := (interchange.SnapDefaults{Character: key(ch), Persona: key(pe)}); d != (interchange.SnapDefaults{}) {
			snap.Defaults = &d
		}
	}
	for _, id := range order {
		if scope == "full" || scope == "cards" || used[id] {
			c := cards[id]
			c.ID, c.Revision, c.Position = 0, 0, 0
			snap.Cards = append(snap.Cards, interchange.SnapCard{Key: "c" + strconv.FormatInt(id, 10), Card: c})
		}
	}
	return snap, tx.Commit()
}

// ImportOptions control the merge. Policy "add" always creates cards (renaming on a
// name collision); "skip" reuses an existing card of the same kind and name.
type ImportOptions struct {
	Policy        string
	ApplyDefaults bool
	DryRun        bool
}

type ImportResult struct {
	CardsAdded         int  `json:"cards_added"`
	CardsSkipped       int  `json:"cards_skipped"`
	CardsRenamed       int  `json:"cards_renamed"`
	ConversationsAdded int  `json:"conversations_added"`
	MessagesAdded      int  `json:"messages_added"`
	DefaultsApplied    bool `json:"defaults_applied"`
}

// importFailpoint lets tests prove a failure after partial writes rolls everything back.
var importFailpoint func(step string) error

func fail(step string) error {
	if importFailpoint != nil {
		return importFailpoint(step)
	}
	return nil
}

// ImportSnapshot applies a validated snapshot in one transaction. A dry run performs
// the real writes and rolls them back, so previews report exact outcomes.
func (s *Store) ImportSnapshot(ctx context.Context, snap *interchange.Snapshot, opt ImportOptions) (res ImportResult, err error) {
	if opt.Policy != "add" && opt.Policy != "skip" {
		return res, ErrInvalidImport
	}
	if err = snap.Validate(); err != nil {
		return res, fmt.Errorf("%w: %v", ErrInvalidImport, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	// Take the write lock first so name checks and inserts see one consistent library.
	if _, err = tx.ExecContext(ctx, `UPDATE cast_settings SET revision=revision WHERE id=1`); err != nil {
		return res, err
	}
	existing := map[string]int64{}
	taken := map[string]bool{}
	next := map[string]int{}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,position,data_json FROM cast_cards`)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var id int64
		var kind, raw string
		var pos int
		if err = rows.Scan(&id, &kind, &pos, &raw); err != nil {
			rows.Close()
			return res, err
		}
		var c cast.Card
		_ = json.Unmarshal([]byte(raw), &c)
		k := kind + "\x00" + strings.ToLower(c.Name)
		taken[k] = true
		if _, ok := existing[k]; !ok {
			existing[k] = id
		}
		if pos+1 > next[kind] {
			next[kind] = pos + 1
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return res, err
	}
	ids := map[string]int64{}
	for _, sc := range snap.Cards {
		card := sc.Card
		k := card.Kind + "\x00" + strings.ToLower(card.Name)
		if opt.Policy == "skip" {
			if id, ok := existing[k]; ok {
				ids[sc.Key] = id
				res.CardsSkipped++
				continue
			}
		} else if taken[k] {
			base := card.Name
			if len(base) > 170 {
				base = base[:170]
			}
			for n := 1; ; n++ {
				suffix := " (imported)"
				if n > 1 {
					suffix = fmt.Sprintf(" (imported %d)", n)
				}
				if !taken[card.Kind+"\x00"+strings.ToLower(base+suffix)] {
					card.Name = base + suffix
					break
				}
			}
			res.CardsRenamed++
		}
		card.ID, card.Revision, card.Position = 0, 0, next[card.Kind]
		next[card.Kind]++
		if err = card.Validate(); err != nil {
			return res, fmt.Errorf("%w: %v", ErrInvalidImport, err)
		}
		raw, merr := json.Marshal(card)
		if merr != nil {
			return res, merr
		}
		r, ierr := tx.ExecContext(ctx, `INSERT INTO cast_cards(kind,position,data_json,updated_at) VALUES(?,?,?,?)`, card.Kind, card.Position, string(raw), formatTime(time.Now().UTC()))
		if ierr != nil {
			return res, ierr
		}
		id, _ := r.LastInsertId()
		ids[sc.Key] = id
		taken[card.Kind+"\x00"+strings.ToLower(card.Name)] = true
		existing[card.Kind+"\x00"+strings.ToLower(card.Name)] = id
		res.CardsAdded++
	}
	if err = fail("cards"); err != nil {
		return res, err
	}
	greetings := map[string]int{}
	for _, sc := range snap.Cards {
		greetings[sc.Key] = len(sc.Card.Greetings)
	}
	now := formatTime(time.Now().UTC())
	for _, c := range snap.Conversations {
		created := now
		if t, perr := parseTime(c.CreatedAt); perr == nil {
			created = formatTime(t)
		}
		r, ierr := tx.ExecContext(ctx, `INSERT INTO conversations(title,endpoint_id,model_id,system_prompt,params_json,created_at,updated_at) VALUES(?,NULL,NULL,?,'{}',?,?)`, c.Title, c.SystemPrompt, created, created)
		if ierr != nil {
			return res, ierr
		}
		convID, _ := r.LastInsertId()
		var chID, peID any
		greeting := c.Greeting
		if c.Character != "" {
			chID = ids[c.Character]
			if greeting >= greetings[c.Character] {
				greeting = 0
			}
		}
		if c.Persona != "" {
			peID = ids[c.Persona]
		}
		if _, ierr = tx.ExecContext(ctx, `INSERT INTO conversation_cast(conversation_id,character_id,persona_id,greeting) VALUES(?,?,?,?)`, convID, chID, peID, greeting); ierr != nil {
			return res, ierr
		}
		var parent any
		for _, m := range c.Messages {
			speaker := "{}"
			if m.Speaker != nil {
				b, _ := json.Marshal(m.Speaker)
				speaker = string(b)
			}
			at := created
			if t, perr := parseTime(m.CreatedAt); perr == nil {
				at = formatTime(t)
			}
			r, ierr := tx.ExecContext(ctx, `INSERT INTO messages(conversation_id,parent_id,role,content,reasoning,status,created_at,speaker_json) VALUES(?,?,?,?,?,?,?,?)`, convID, parent, m.Role, m.Content, m.Reasoning, m.Status, at, speaker)
			if ierr != nil {
				return res, ierr
			}
			id, _ := r.LastInsertId()
			parent = id
			res.MessagesAdded++
		}
		res.ConversationsAdded++
	}
	if err = fail("conversations"); err != nil {
		return res, err
	}
	if opt.ApplyDefaults && snap.Defaults != nil {
		var ch, pe any
		if snap.Defaults.Character != "" {
			ch = ids[snap.Defaults.Character]
		}
		if snap.Defaults.Persona != "" {
			pe = ids[snap.Defaults.Persona]
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cast_settings SET character_id=?,persona_id=?,revision=revision+1 WHERE id=1`, ch, pe); err != nil {
			return res, err
		}
		res.DefaultsApplied = true
	}
	if opt.DryRun {
		return res, nil // deferred Rollback discards every write
	}
	return res, tx.Commit()
}

// ResetData deletes user content by scope: "conversations", "cast" or "all". Endpoints,
// credentials and model inventory are never touched. Callers must back up first.
func (s *Store) ResetData(ctx context.Context, scope string) error {
	if scope != "conversations" && scope != "cast" && scope != "all" {
		return ErrInvalidImport
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if scope == "conversations" || scope == "all" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM conversations`); err != nil {
			return err
		}
	}
	if scope == "cast" || scope == "all" {
		if _, err = tx.ExecContext(ctx, `UPDATE conversation_cast SET revision=revision+1`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cast_settings SET character_id=NULL,persona_id=NULL,revision=revision+1`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM cast_cards`); err != nil {
			return err
		}
	}
	return tx.Commit()
}
