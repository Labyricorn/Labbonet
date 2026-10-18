package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Labyricorn/Labbonet/internal/profile"
)

// ErrInvalidProfile reports a rejected profile.
var ErrInvalidProfile = errors.New("invalid profile request")

func scanProfile(sc interface{ Scan(...any) error }) (profile.Profile, error) {
	var p profile.Profile
	var raw string
	if err := sc.Scan(&p.ID, &p.Name, &raw, &p.Revision, &p.UpdatedAt); err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(raw), &p.Data); err != nil {
		return p, fmt.Errorf("read profile %d: %w", p.ID, err)
	}
	return p, nil
}

// ListProfiles returns every profile by name.
func (s *Store) ListProfiles(ctx context.Context) ([]profile.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,data_json,revision,updated_at FROM profiles ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []profile.Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProfile returns one profile (sql.ErrNoRows if absent).
func (s *Store) GetProfile(ctx context.Context, id int64) (profile.Profile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `SELECT id,name,data_json,revision,updated_at FROM profiles WHERE id=?`, id))
}

// SaveProfile creates (ID 0) or updates a profile; an update needs the revision it loaded.
// Names are unique ignoring case.
func (s *Store) SaveProfile(ctx context.Context, in profile.Profile, now time.Time) (profile.Profile, error) {
	if err := in.Validate(); err != nil {
		return in, fmt.Errorf("%w: %v", ErrInvalidProfile, err)
	}
	raw, err := json.Marshal(in.Data)
	if err != nil {
		return in, err
	}
	stamp := formatTime(now)
	if in.ID == 0 {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM profiles`).Scan(&n); err != nil {
			return in, err
		}
		if n >= profile.MaxProfiles {
			return in, fmt.Errorf("%w: at most %d profiles", ErrInvalidProfile, profile.MaxProfiles)
		}
		res, err := s.db.ExecContext(ctx, `INSERT INTO profiles(name,data_json,updated_at) VALUES(?,?,?)`, in.Name, string(raw), stamp)
		if err != nil {
			return in, profileNameError(err)
		}
		id, _ := res.LastInsertId()
		return s.GetProfile(ctx, id)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE profiles SET name=?,data_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, in.Name, string(raw), stamp, in.ID, in.Revision)
	if err != nil {
		return in, profileNameError(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var one int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM profiles WHERE id=?`, in.ID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return in, sql.ErrNoRows
		}
		return in, ErrConflict
	}
	return s.GetProfile(ctx, in.ID)
}

func profileNameError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: a profile with that name exists", ErrInvalidProfile)
	}
	return err
}

// DeleteProfile removes a profile; the active marker clears if it pointed here.
func (s *Store) DeleteProfile(ctx context.Context, id, revision int64) error {
	q, args := `DELETE FROM profiles WHERE id=?`, []any{id}
	if revision > 0 {
		q, args = q+` AND revision=?`, append(args, revision)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var one int
		if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM profiles WHERE id=?`, id).Scan(&one); errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return ErrConflict
	}
	return nil
}

// ActiveProfile returns the id of the profile last applied, or 0.
func (s *Store) ActiveProfile(ctx context.Context) (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT active_id FROM profile_state WHERE id=1`).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

// SetActiveProfile records which profile was last applied (0 clears it).
func (s *Store) SetActiveProfile(ctx context.Context, id int64) error {
	var v any
	if id > 0 {
		v = id
	}
	_, err := s.db.ExecContext(ctx, `UPDATE profile_state SET active_id=? WHERE id=1`, v)
	return err
}
