package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

var ErrConflict = errors.New("revision conflict; reload before saving")
var ErrInvalidCast = errors.New("invalid cast selection")

func (s *Store) ListCards(ctx context.Context) ([]cast.Card, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,revision,position,data_json FROM cast_cards ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cast.Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}
func scanCard(row rowScanner) (*cast.Card, error) {
	var c cast.Card
	var raw string
	if err := row.Scan(&c.ID, &c.Kind, &c.Revision, &c.Position, &raw); err != nil {
		return nil, err
	}
	id, kind, rev, pos := c.ID, c.Kind, c.Revision, c.Position
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	c.ID, c.Kind, c.Revision, c.Position = id, kind, rev, pos
	return &c, nil
}
func (s *Store) GetCard(ctx context.Context, id int64) (*cast.Card, error) {
	return scanCard(s.db.QueryRowContext(ctx, `SELECT id,kind,revision,position,data_json FROM cast_cards WHERE id=?`, id))
}
func (s *Store) SaveCard(ctx context.Context, c cast.Card) (*cast.Card, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if c.ID == 0 {
		res, err := s.db.ExecContext(ctx, `INSERT INTO cast_cards(kind,position,data_json,updated_at) VALUES(?,?,?,?)`, c.Kind, c.Position, string(raw), formatTime(time.Now().UTC()))
		if err != nil {
			return nil, err
		}
		c.ID, err = res.LastInsertId()
		if err != nil {
			return nil, err
		}
	} else {
		res, err := s.db.ExecContext(ctx, `UPDATE cast_cards SET data_json=?,position=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND kind=?`, string(raw), c.Position, formatTime(time.Now().UTC()), c.ID, c.Revision, c.Kind)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, ErrConflict
		}
	}
	return s.GetCard(ctx, c.ID)
}
func (s *Store) DeleteCard(ctx context.Context, id, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Invalidate dependent editors before FK SET NULL clears selections.
	if _, err = tx.ExecContext(ctx, `UPDATE conversation_cast SET revision=revision+1 WHERE character_id=? OR persona_id=?`, id, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cast_settings SET revision=revision+1 WHERE character_id=? OR persona_id=?`, id, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM cast_cards WHERE id=? AND revision=?`, id, revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

type CastSelection struct {
	Revision    int64  `json:"revision"`
	CharacterID *int64 `json:"character_id"`
	PersonaID   *int64 `json:"persona_id"`
	Greeting    int    `json:"greeting"`
}

type CardOrder struct {
	ID       int64 `json:"id"`
	Revision int64 `json:"revision"`
}

// ReorderCards applies the complete order for one kind atomically. Stale lists
// (including concurrent additions/deletions) cannot partly reorder the library.
func (s *Store) ReorderCards(ctx context.Context, kind string, order []CardOrder) error {
	if (kind != "character" && kind != "persona") || len(order) > 10000 {
		return ErrInvalidCast
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE cast_settings SET revision=revision WHERE id=1`); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cast_cards WHERE kind=?`, kind).Scan(&count); err != nil {
		return err
	}
	if count != len(order) {
		return ErrConflict
	}
	seen := map[int64]bool{}
	for i, ref := range order {
		if seen[ref.ID] {
			return ErrInvalidCast
		}
		seen[ref.ID] = true
		res, err := tx.ExecContext(ctx, `UPDATE cast_cards SET position=?,revision=revision+1 WHERE id=? AND revision=? AND kind=?`, i, ref.ID, ref.Revision, kind)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrConflict
		}
	}
	return tx.Commit()
}

// GetCast initializes legacy conversations without a cast; defaults apply only at creation.
func (s *Store) GetCast(ctx context.Context, conversationID int64) (CastSelection, error) {
	if conversationID == 0 {
		var c CastSelection
		err := s.db.QueryRowContext(ctx, `SELECT revision,character_id,persona_id FROM cast_settings WHERE id=1`).Scan(&c.Revision, &c.CharacterID, &c.PersonaID)
		return c, err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversation_cast(conversation_id,character_id,persona_id) SELECT id,NULL,NULL FROM conversations WHERE id=? ON CONFLICT DO NOTHING`, conversationID)
	if err != nil {
		return CastSelection{}, err
	}
	var c CastSelection
	err = s.db.QueryRowContext(ctx, `SELECT revision,character_id,persona_id,greeting FROM conversation_cast WHERE conversation_id=?`, conversationID).Scan(&c.Revision, &c.CharacterID, &c.PersonaID, &c.Greeting)
	return c, err
}
func (s *Store) SaveCast(ctx context.Context, conversationID int64, c CastSelection) (CastSelection, error) {
	if c.Greeting < 0 || c.Greeting > 63 {
		return c, ErrInvalidCast
	}
	if _, err := s.GetCast(ctx, conversationID); err != nil {
		return c, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	for _, v := range []struct {
		id   *int64
		kind string
	}{{c.CharacterID, "character"}, {c.PersonaID, "persona"}} {
		if v.id != nil {
			var kind string
			if err := tx.QueryRowContext(ctx, `SELECT kind FROM cast_cards WHERE id=?`, *v.id).Scan(&kind); err != nil || kind != v.kind {
				return c, ErrInvalidCast
			}
		}
	}
	if c.CharacterID == nil && c.Greeting != 0 {
		return c, ErrInvalidCast
	}
	if c.CharacterID != nil && c.Greeting > 0 {
		card, err := scanCard(tx.QueryRowContext(ctx, `SELECT id,kind,revision,position,data_json FROM cast_cards WHERE id=?`, *c.CharacterID))
		if err != nil {
			return c, err
		}
		if c.Greeting >= len(card.Greetings) {
			return c, ErrInvalidCast
		}
	}
	var res sql.Result
	if conversationID == 0 {
		res, err = tx.ExecContext(ctx, `UPDATE cast_settings SET character_id=?,persona_id=?,revision=revision+1 WHERE id=1 AND revision=?`, c.CharacterID, c.PersonaID, c.Revision)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE conversation_cast SET character_id=?,persona_id=?,greeting=?,revision=revision+1 WHERE conversation_id=? AND revision=?`, c.CharacterID, c.PersonaID, c.Greeting, conversationID, c.Revision)
	}
	if err != nil {
		return c, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return c, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return c, err
	}
	c.Revision++
	return c, nil
}

// ReadCast resolves a single consistent snapshot, even when another tab edits cards.
func (s *Store) ReadCast(ctx context.Context, id int64) (CastSelection, *cast.Card, *cast.Card, error) {
	if _, err := s.GetCast(ctx, id); err != nil {
		return CastSelection{}, nil, nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CastSelection{}, nil, nil, err
	}
	defer tx.Rollback()
	var sel CastSelection
	err = tx.QueryRowContext(ctx, `SELECT revision,character_id,persona_id,greeting FROM conversation_cast WHERE conversation_id=?`, id).Scan(&sel.Revision, &sel.CharacterID, &sel.PersonaID, &sel.Greeting)
	if err != nil {
		return sel, nil, nil, err
	}
	var char, persona *cast.Card
	for _, v := range []struct {
		id  *int64
		dst **cast.Card
	}{{sel.CharacterID, &char}, {sel.PersonaID, &persona}} {
		if v.id != nil {
			*v.dst, err = scanCard(tx.QueryRowContext(ctx, `SELECT id,kind,revision,position,data_json FROM cast_cards WHERE id=?`, *v.id))
			if err != nil {
				return sel, nil, nil, err
			}
		}
	}
	return sel, char, persona, tx.Commit()
}
