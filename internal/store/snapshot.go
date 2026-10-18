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
	if scope != "full" && scope != "cards" && scope != "conversation" && scope != "threads" {
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
		q := `SELECT id,title,system_prompt,created_at,active_leaf_id,pinned,(SELECT name FROM folders WHERE folders.id=conversations.folder_id) FROM conversations`
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
			var leaf sql.NullInt64
			var pinned int
			var folder sql.NullString
			if err := crows.Scan(&h.id, &h.convo.Title, &h.convo.SystemPrompt, &created, &leaf, &pinned, &folder); err != nil {
				crows.Close()
				return nil, err
			}
			h.convo.Pinned = pinned != 0
			h.convo.Folder = folder.String
			if leaf.Valid {
				h.convo.Active = "m" + strconv.FormatInt(leaf.Int64, 10)
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
			trows, err := tx.QueryContext(ctx, `SELECT tag FROM conversation_tags WHERE conversation_id=? ORDER BY tag COLLATE NOCASE`, h.id)
			if err != nil {
				return nil, err
			}
			for trows.Next() {
				var tag string
				if err := trows.Scan(&tag); err != nil {
					trows.Close()
					return nil, err
				}
				h.convo.Tags = append(h.convo.Tags, tag)
			}
			trows.Close()
			if err := trows.Err(); err != nil {
				return nil, err
			}
			mrows, err := tx.QueryContext(ctx, `SELECT id,parent_id,selected_child_id,origin,origin_id,role,content,reasoning,status,created_at,speaker_json FROM messages WHERE conversation_id=? ORDER BY id`, h.id)
			if err != nil {
				return nil, err
			}
			h.convo.Messages = []interchange.SnapMessage{}
			for mrows.Next() {
				var m interchange.SnapMessage
				var speaker string
				var msgDBID int64
				var parent, selected, originID sql.NullInt64
				if err := mrows.Scan(&msgDBID, &parent, &selected, &m.Origin, &originID, &m.Role, &m.Content, &m.Reasoning, &m.Status, &m.CreatedAt, &speaker); err != nil {
					mrows.Close()
					return nil, err
				}
				m.Key = "m" + strconv.FormatInt(msgDBID, 10)
				if parent.Valid {
					m.Parent = "m" + strconv.FormatInt(parent.Int64, 10)
				}
				if selected.Valid {
					m.Selected = "m" + strconv.FormatInt(selected.Int64, 10)
				}
				if originID.Valid {
					m.OriginKey = "m" + strconv.FormatInt(originID.Int64, 10)
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
	FoldersAdded       int  `json:"folders_added"`
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
	folders := map[string]int64{}
	frows, err := tx.QueryContext(ctx, `SELECT id,name FROM folders`)
	if err != nil {
		return res, err
	}
	for frows.Next() {
		var id int64
		var name string
		if err = frows.Scan(&id, &name); err != nil {
			frows.Close()
			return res, err
		}
		folders[strings.ToLower(name)] = id
	}
	frows.Close()
	if err = frows.Err(); err != nil {
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
		var folderID any
		if name := strings.Join(strings.Fields(c.Folder), " "); name != "" {
			if id, ok := folders[strings.ToLower(name)]; ok {
				folderID = id
			} else {
				fr, ferr := tx.ExecContext(ctx, `INSERT INTO folders(name,position,created_at) VALUES(?,(SELECT COALESCE(MAX(position)+1,0) FROM folders),?)`, name, now)
				if ferr != nil {
					return res, ferr
				}
				fid, _ := fr.LastInsertId()
				folders[strings.ToLower(name)] = fid
				folderID = fid
				res.FoldersAdded++
			}
		}
		pinned := 0
		if c.Pinned {
			pinned = 1
		}
		r, ierr := tx.ExecContext(ctx, `INSERT INTO conversations(title,endpoint_id,model_id,system_prompt,params_json,created_at,updated_at,pinned,folder_id) VALUES(?,NULL,NULL,?,'{}',?,?,?,?)`, c.Title, c.SystemPrompt, created, created, pinned, folderID)
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
		keyIDs := map[string]int64{}
		for _, m := range c.Messages {
			var parent any
			if m.Parent != "" {
				parent = keyIDs[m.Parent]
			}
			var originID any
			if m.OriginKey != "" {
				originID = keyIDs[m.OriginKey]
			}
			origin := m.Origin
			if origin == "" {
				origin = "original"
			}
			speaker := "{}"
			if m.Speaker != nil {
				b, _ := json.Marshal(m.Speaker)
				speaker = string(b)
			}
			at := created
			if t, perr := parseTime(m.CreatedAt); perr == nil {
				at = formatTime(t)
			}
			r, ierr := tx.ExecContext(ctx, `INSERT INTO messages(conversation_id,parent_id,role,content,reasoning,status,created_at,speaker_json,origin,origin_id) VALUES(?,?,?,?,?,?,?,?,?,?)`, convID, parent, m.Role, m.Content, m.Reasoning, m.Status, at, speaker, origin, originID)
			if ierr != nil {
				return res, ierr
			}
			id, _ := r.LastInsertId()
			keyIDs[m.Key] = id
			res.MessagesAdded++
		}
		for _, m := range c.Messages {
			if m.Selected == "" {
				continue
			}
			if _, ierr = tx.ExecContext(ctx, `UPDATE messages SET selected_child_id=? WHERE id=?`, keyIDs[m.Selected], keyIDs[m.Key]); ierr != nil {
				return res, ierr
			}
		}
		if c.Active != "" {
			if _, ierr = tx.ExecContext(ctx, `UPDATE conversations SET active_leaf_id=? WHERE id=?`, keyIDs[c.Active], convID); ierr != nil {
				return res, ierr
			}
		}
		tags, terr := NormalizeTags(c.Tags)
		if terr != nil {
			return res, fmt.Errorf("%w: %v", ErrInvalidImport, terr)
		}
		for _, tag := range tags {
			if _, ierr = tx.ExecContext(ctx, `INSERT INTO conversation_tags(conversation_id,tag) VALUES(?,?)`, convID, tag); ierr != nil {
				return res, ierr
			}
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
		if _, err = tx.ExecContext(ctx, `DELETE FROM folders`); err != nil {
			return err
		}
		// Summaries and retrieval records go with their conversations (cascade); the
		// vector cache holds derived data only and is cleared with a full reset.
		if scope == "all" {
			if _, err = tx.ExecContext(ctx, `DELETE FROM embedding_cache`); err != nil {
				return err
			}
		}
	}
	if scope == "all" {
		// MODS data is content, not credentials: macros and appearance go; the web-search
		// key and its settings stay, like endpoint credentials.
		if _, err = tx.ExecContext(ctx, `DELETE FROM macros`); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE appearance_settings SET data_json='{}',revision=revision+1 WHERE id=1`); err != nil {
			return err
		}
		// Schedules and profiles are settings content too; their ledgers cascade.
		for _, q := range []string{`DELETE FROM schedules`, `DELETE FROM profiles`} {
			if _, err = tx.ExecContext(ctx, q); err != nil {
				return err
			}
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
