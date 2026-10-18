package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Labyricorn/Labbonet/internal/mods"
	"github.com/Labyricorn/Labbonet/internal/search"
	_ "modernc.org/sqlite"
)

func TestMacrosCRUDRevisionsAndSetValidation(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)

	a, err := st.SaveMacro(ctx, mods.Macro{Trigger: "wrap_up", Text: "please conclude"})
	if err != nil || a.ID == 0 || a.Revision != 1 {
		t.Fatalf("create = %+v, %v", a, err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{Trigger: "WRAP_UP", Text: "dup"}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("duplicate ignoring case accepted: %v", err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{Trigger: "char", Text: "x"}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("reserved name accepted: %v", err)
	}
	// An update must echo the revision it edited; the trigger cannot change.
	edited, err := st.SaveMacro(ctx, mods.Macro{ID: a.ID, Revision: a.Revision, Trigger: "wrap_up", Text: "conclude now"})
	if err != nil || edited.Revision != 2 || edited.Text != "conclude now" {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{ID: a.ID, Revision: a.Revision, Trigger: "wrap_up", Text: "stale tab"}); !errors.Is(err, ErrConflict) {
		t.Errorf("stale edit = %v", err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{ID: a.ID, Revision: 2, Trigger: "renamed", Text: "x"}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("rename accepted: %v", err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{ID: 999, Revision: 1, Trigger: "ghost", Text: "x"}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("missing macro = %v", err)
	}

	// A change that would make a loop is refused and leaves the set as it was.
	b, err := st.SaveMacro(ctx, mods.Macro{Trigger: "outer", Text: "see {{wrap_up}}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveMacro(ctx, mods.Macro{ID: a.ID, Revision: 2, Trigger: "wrap_up", Text: "back to {{outer}}"}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("cycle accepted: %v", err)
	}
	list, err := st.ListMacros(ctx)
	if err != nil || len(list) != 2 || list[0].Trigger != "outer" || list[1].Text != "conclude now" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err = st.DeleteMacro(ctx, b.ID, 99); !errors.Is(err, ErrConflict) {
		t.Errorf("stale delete = %v", err)
	}
	if err = st.DeleteMacro(ctx, b.ID, b.Revision); err != nil {
		t.Fatal(err)
	}
	if err = st.DeleteMacro(ctx, b.ID, 0); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("second delete = %v", err)
	}
}

func TestMacroLimitsAreEnforcedByTheStore(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	if _, err := st.SaveMacro(ctx, mods.Macro{Trigger: "big", Text: strings.Repeat("x", mods.MaxMacroBytes+1)}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("oversized macro accepted: %v", err)
	}
	for i := 0; i < mods.MaxMacros; i++ {
		if _, err := st.SaveMacro(ctx, mods.Macro{Trigger: "m" + strings.Repeat("a", i/26) + string(rune('a'+i%26)), Text: "x"}); err != nil {
			t.Fatalf("macro %d: %v", i, err)
		}
	}
	if _, err := st.SaveMacro(ctx, mods.Macro{Trigger: "one_too_many", Text: "x"}); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("macro past the limit accepted: %v", err)
	}
}

func TestAppearanceRevisionIDsAndValidation(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	cur, err := st.GetAppearance(ctx)
	if err != nil || cur.Revision != 1 || cur.Active != "" || len(cur.Presets) != 0 {
		t.Fatalf("defaults = %+v, %v", cur, err)
	}
	next := cur
	next.Presets = []mods.Preset{{Name: " Mine ", Vars: map[string]string{"--magenta": "#ff00aa"}, Builtin: true}}
	saved, err := st.SaveAppearance(ctx, next)
	if err != nil || saved.Revision != 2 || len(saved.Presets) != 1 {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	p := saved.Presets[0]
	if !strings.HasPrefix(p.ID, "c-") || p.Name != "Mine" || p.Builtin {
		t.Errorf("preset not normalised: %+v", p)
	}
	active := saved
	active.Active = p.ID
	if saved, err = st.SaveAppearance(ctx, active); err != nil || saved.Active != p.ID {
		t.Fatalf("activate = %+v, %v", saved, err)
	}
	if _, err = st.SaveAppearance(ctx, active); !errors.Is(err, ErrConflict) {
		t.Errorf("stale save = %v", err)
	}
	bad := saved
	bad.Presets = []mods.Preset{{ID: p.ID, Name: "x", Vars: map[string]string{"--black": "url(http://example.org/a)"}}}
	if _, err = st.SaveAppearance(ctx, bad); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("unsafe preset accepted: %v", err)
	}
	bad = saved
	bad.Active = "ghost"
	if _, err = st.SaveAppearance(ctx, bad); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("unknown active accepted: %v", err)
	}
}

func TestSearchSettingsKeyAndRecords(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	cur, err := st.GetSearchSettings(ctx)
	if err != nil || cur.Enabled || cur.MaxResults != search.DefaultResults || cur.Revision != 1 {
		t.Fatalf("defaults = %+v, %v (search must start off)", cur, err)
	}
	if blob, _ := st.SearchCredentialBlob(ctx); len(blob) != 0 {
		t.Fatal("a fresh database has a search key")
	}
	bad := cur
	bad.MaxResults = 99
	if _, err = st.SaveSearchSettings(ctx, bad); !errors.Is(err, ErrInvalidMods) {
		t.Errorf("out-of-range results accepted: %v", err)
	}
	on := cur
	on.Enabled, on.MaxResults = true, 3
	saved, err := st.SaveSearchSettings(ctx, on)
	if err != nil || !saved.Enabled || saved.MaxResults != 3 || saved.Revision != 2 {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	if _, err = st.SaveSearchSettings(ctx, on); !errors.Is(err, ErrConflict) {
		t.Errorf("stale save = %v", err)
	}
	// Saving settings never touches the key.
	if err = st.SetSearchCredential(ctx, []byte("opaque")); err != nil {
		t.Fatal(err)
	}
	next := saved
	next.MaxResults = 4
	if _, err = st.SaveSearchSettings(ctx, next); err != nil {
		t.Fatal(err)
	}
	if blob, _ := st.SearchCredentialBlob(ctx); string(blob) != "opaque" {
		t.Errorf("settings save altered the key: %q", blob)
	}
	if err = st.ClearSearchCredential(ctx); err != nil {
		t.Fatal(err)
	}
	if blob, _ := st.SearchCredentialBlob(ctx); len(blob) != 0 {
		t.Error("key not cleared")
	}

	conv, err := st.CreateConversation(ctx, "T", nil, nil, "", "{}")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < searchPerConv+5; i++ {
		if err = st.AddSearchRecord(ctx, conv.ID, nil, map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := st.ListSearchRecords(ctx, conv.ID, 0)
	if err != nil || len(recs) != searchPerConv || string(recs[0].Record) != `{"n":54}` {
		t.Fatalf("records = %d (newest %s), %v", len(recs), recs[0].Record, err)
	}
	if err = st.DeleteConversation(ctx, conv.ID); err != nil {
		t.Fatal(err)
	}
	if recs, _ = st.ListSearchRecords(ctx, conv.ID, 0); len(recs) != 0 {
		t.Errorf("records outlived their conversation: %d", len(recs))
	}
}

// The web-search key is a credential: a backup copy must not carry it, but the live
// database keeps it.
func TestBackupDropsTheSearchKeyButTheLiveStoreKeepsIt(t *testing.T) {
	ctx := t.Context()
	st, dir := newTestStore(t)
	if err := st.SetSearchCredential(ctx, []byte("protected-blob")); err != nil {
		t.Fatal(err)
	}
	path, err := st.Backup(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var blob []byte
	if err = db.QueryRow(`SELECT credential_blob FROM search_settings WHERE id=1`).Scan(&blob); err != nil || len(blob) != 0 {
		t.Fatalf("backup carries the key: %q, %v", blob, err)
	}
	if live, _ := st.SearchCredentialBlob(ctx); string(live) != "protected-blob" {
		t.Errorf("live key lost: %q", live)
	}
}

func TestResetAllClearsMacrosAndAppearanceButKeepsTheSearchKey(t *testing.T) {
	ctx := t.Context()
	st, _ := newTestStore(t)
	if _, err := st.SaveMacro(ctx, mods.Macro{Trigger: "m", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	cur, _ := st.GetAppearance(ctx)
	cur.Active = "amber"
	if _, err := st.SaveAppearance(ctx, cur); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSearchCredential(ctx, []byte("k")); err != nil {
		t.Fatal(err)
	}
	if err := st.ResetData(ctx, "conversations"); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListMacros(ctx); len(list) != 1 {
		t.Error("a conversations reset removed macros")
	}
	if err := st.ResetData(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.ListMacros(ctx); len(list) != 0 {
		t.Error("macros survived a full reset")
	}
	if a, _ := st.GetAppearance(ctx); a.Active != "" || a.Revision < 3 {
		t.Errorf("appearance after reset = %+v", a)
	}
	if blob, _ := st.SearchCredentialBlob(ctx); string(blob) != "k" {
		t.Error("a full reset removed the search key")
	}
}

// A populated schema-4 database upgrades to schema 5 with its data intact. The pre-upgrade
// backup is taken before search_settings exists, so it must not choke on the missing table.
func TestModsMigrationPreservesExistingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []migrationFile{{1, "0001_init.sql"}, {2, "0002_cast.sql"}, {3, "0003_branches.sql"}, {4, "0004_memory.sql"}} {
		if err = applyMigration(db, f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO conversations(title,params_json,created_at,updated_at) VALUES('keep','{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if convs, err := st.ListConversations(t.Context()); err != nil || len(convs) != 1 || convs[0].Title != "keep" {
		t.Fatalf("convs=%+v err=%v", convs, err)
	}
	if backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "*")); len(backups) != 1 {
		t.Fatalf("pre-upgrade backup missing: %v", backups)
	}
	if s, err := st.GetSearchSettings(t.Context()); err != nil || s.Enabled || s.Revision != 1 {
		t.Fatalf("search settings=%+v err=%v", s, err)
	}
	if a, err := st.GetAppearance(t.Context()); err != nil || a.Revision != 1 {
		t.Fatalf("appearance=%+v err=%v", a, err)
	}
}
