package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/mods"
	"github.com/Labyricorn/Labbonet/internal/search"
)

// ErrInvalidMods reports a rejected macro, preset or search setting.
var ErrInvalidMods = errors.New("invalid MODS or search request")

const searchPerConv = 50

func invalidMods(err error) error { return fmt.Errorf("%w: %v", ErrInvalidMods, err) }

// ---- search settings and key ---------------------------------------------------------

// GetSearchSettings returns the stored settings merged over the defaults.
func (s *Store) GetSearchSettings(ctx context.Context) (search.Settings, error) {
	var raw string
	var rev int64
	if err := s.db.QueryRowContext(ctx, `SELECT revision,data_json FROM search_settings WHERE id=1`).Scan(&rev, &raw); err != nil {
		return search.Settings{}, err
	}
	out := search.Defaults()
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return search.Settings{}, fmt.Errorf("read search settings: %w", err)
		}
	}
	out.Revision = rev
	return out, nil
}

// SaveSearchSettings validates and saves settings if Revision still matches.
func (s *Store) SaveSearchSettings(ctx context.Context, in search.Settings) (search.Settings, error) {
	if err := in.Validate(); err != nil {
		return in, invalidMods(err)
	}
	rev := in.Revision
	in.Revision = 0
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE search_settings SET data_json=?,revision=revision+1 WHERE id=1 AND revision=?`, string(raw), rev)
	if err != nil {
		return in, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return in, ErrConflict
	}
	return s.GetSearchSettings(ctx)
}

// SetSearchCredential stores (or replaces) the protected key blob. The blob is opaque
// here; protecting it is internal/secrets' job.
func (s *Store) SetSearchCredential(ctx context.Context, blob []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE search_settings SET credential_blob=? WHERE id=1`, blob)
	if err != nil {
		return fmt.Errorf("set search credential: %w", err)
	}
	return nil
}

// ClearSearchCredential removes the stored key.
func (s *Store) ClearSearchCredential(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE search_settings SET credential_blob=NULL WHERE id=1`)
	return err
}

// SearchCredentialBlob returns the protected blob, or nil when no key is stored. Callers
// outside internal/secrets must never log or serialise it.
func (s *Store) SearchCredentialBlob(ctx context.Context) ([]byte, error) {
	var blob []byte
	if err := s.db.QueryRowContext(ctx, `SELECT credential_blob FROM search_settings WHERE id=1`).Scan(&blob); err != nil {
		return nil, err
	}
	return blob, nil
}

// SearchRecord is one stored search outcome (the JSON is generate.SearchRecord).
type SearchRecord struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversation_id"`
	GenerationID   *int64          `json:"generation_id"`
	MessageID      *int64          `json:"message_id"`
	Record         json.RawMessage `json:"record"`
	CreatedAt      time.Time       `json:"created_at"`
}

// AddSearchRecord stores one record and keeps only the newest few per conversation.
func (s *Store) AddSearchRecord(ctx context.Context, convID int64, generationID *int64, record any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO search_records(conversation_id,generation_id,record_json,created_at) VALUES(?,?,?,?)`, convID, generationID, string(raw), formatTime(time.Now().UTC())); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search_records WHERE conversation_id=? AND id NOT IN (SELECT id FROM search_records WHERE conversation_id=? ORDER BY id DESC LIMIT ?)`, convID, convID, searchPerConv); err != nil {
		return err
	}
	return tx.Commit()
}

// ListSearchRecords returns the newest records first, each with the assistant message
// that its generation produced (nil if that message was since deleted).
func (s *Store) ListSearchRecords(ctx context.Context, convID int64, limit int) ([]SearchRecord, error) {
	if limit <= 0 || limit > searchPerConv {
		limit = searchPerConv
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.conversation_id,r.generation_id,g.message_id,r.record_json,r.created_at
		FROM search_records r LEFT JOIN generations g ON g.id=r.generation_id
		WHERE r.conversation_id=? ORDER BY r.id DESC LIMIT ?`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchRecord{}
	for rows.Next() {
		var r SearchRecord
		var raw, at string
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.GenerationID, &r.MessageID, &raw, &at); err != nil {
			return nil, err
		}
		r.Record = json.RawMessage(raw)
		if r.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- macros ---------------------------------------------------------------------------

func scanMacro(row rowScanner) (mods.Macro, error) {
	var m mods.Macro
	err := row.Scan(&m.ID, &m.Trigger, &m.Text, &m.Revision, &m.UpdatedAt)
	return m, err
}

// ListMacros returns every macro ordered by trigger.
func (s *Store) ListMacros(ctx context.Context) ([]mods.Macro, error) {
	return listMacros(ctx, s.db)
}

func listMacros(ctx context.Context, q dbq) ([]mods.Macro, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,name,body,revision,updated_at FROM macros ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []mods.Macro{}
	for rows.Next() {
		m, err := scanMacro(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveMacro creates (ID 0) or updates a macro. The whole resulting set is validated, so a
// change that would create an expansion cycle or exceed the limits is refused. An update
// must echo the Revision it edited. The trigger of an existing macro cannot change
// (rename = delete + add).
func (s *Store) SaveMacro(ctx context.Context, in mods.Macro) (*mods.Macro, error) {
	in.Trigger = strings.TrimSpace(in.Trigger)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	existing, err := listMacros(ctx, tx)
	if err != nil {
		return nil, err
	}
	next := make([]mods.Macro, 0, len(existing)+1)
	var current *mods.Macro
	for i := range existing {
		if existing[i].ID == in.ID {
			current = &existing[i]
			continue
		}
		next = append(next, existing[i])
	}
	if in.ID != 0 {
		if current == nil {
			return nil, sql.ErrNoRows
		}
		if in.Revision != current.Revision {
			return nil, ErrConflict
		}
		if !strings.EqualFold(in.Trigger, current.Trigger) {
			return nil, invalidMods(errors.New("a macro's trigger cannot be changed; delete it and add a new one"))
		}
		in.Trigger = current.Trigger
	}
	next = append(next, in)
	if err := mods.ValidateSet(next); err != nil {
		return nil, invalidMods(err)
	}
	now := formatTime(time.Now().UTC())
	if in.ID == 0 {
		res, err := tx.ExecContext(ctx, `INSERT INTO macros(name,body,revision,updated_at) VALUES(?,?,1,?)`, in.Trigger, in.Text, now)
		if err != nil {
			return nil, err
		}
		if in.ID, err = res.LastInsertId(); err != nil {
			return nil, err
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE macros SET body=?,revision=revision+1,updated_at=? WHERE id=?`, in.Text, now, in.ID); err != nil {
		return nil, err
	}
	saved, err := scanMacro(tx.QueryRowContext(ctx, `SELECT id,name,body,revision,updated_at FROM macros WHERE id=?`, in.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &saved, nil
}

// DeleteMacro removes a macro. revision, when nonzero, must match.
func (s *Store) DeleteMacro(ctx context.Context, id, revision int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM macros WHERE id=? AND (?=0 OR revision=?)`, id, revision, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM macros WHERE id=?`, id).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	return ErrConflict
}

// ---- appearance -----------------------------------------------------------------------

// GetAppearance returns the stored selection and custom presets.
func (s *Store) GetAppearance(ctx context.Context) (mods.Appearance, error) {
	var raw string
	var rev int64
	if err := s.db.QueryRowContext(ctx, `SELECT revision,data_json FROM appearance_settings WHERE id=1`).Scan(&rev, &raw); err != nil {
		return mods.Appearance{}, err
	}
	out := mods.Appearance{Presets: []mods.Preset{}}
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return mods.Appearance{}, fmt.Errorf("read appearance: %w", err)
		}
	}
	if out.Presets == nil {
		out.Presets = []mods.Preset{}
	}
	out.Revision = rev
	return out, nil
}

// SaveAppearance validates and saves the selection if Revision still matches. Custom
// presets without an ID get a fresh random one.
func (s *Store) SaveAppearance(ctx context.Context, in mods.Appearance) (mods.Appearance, error) {
	for i := range in.Presets {
		in.Presets[i].Builtin = false
		in.Presets[i].Name = strings.TrimSpace(in.Presets[i].Name)
		if in.Presets[i].Vars == nil {
			in.Presets[i].Vars = map[string]string{}
		}
		if in.Presets[i].ID == "" {
			var b [5]byte
			if _, err := rand.Read(b[:]); err != nil {
				return in, err
			}
			in.Presets[i].ID = "c-" + hex.EncodeToString(b[:])
		}
	}
	if err := in.Validate(); err != nil {
		return in, invalidMods(err)
	}
	rev := in.Revision
	in.Revision = 0
	raw, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE appearance_settings SET data_json=?,revision=revision+1 WHERE id=1 AND revision=?`, string(raw), rev)
	if err != nil {
		return in, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return in, ErrConflict
	}
	return s.GetAppearance(ctx)
}
