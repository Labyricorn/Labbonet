# Labbonet Devlog

Operational memory: implementation discoveries, rejected approaches, debugging outcomes. Durable contracts live in AGENTS.md files, not here.

---

## 2026-10-17 — Phase 1 step 1: skeleton and security shell

**Built:** `go.mod` (module `github.com/Labyricorn/Labbonet`); `cmd/labbonet` (flags, startup, graceful shutdown, best-effort `--open`); `internal/server` (loopback listener, Host allowlist, Origin/`Sec-Fetch-Site`/`X-Labbonet` checks, session cookie middleware, security headers incl. the Phase 1 CSP, one-time launch-code session flow, data-dir lock file) and its `servertest` helper; `internal/api/health.go`; `frontend/` placeholder (`index.html`, `app.css`, `app.js`, embedded via `frontend/embed.go`).

**Tests:** unit tests for S1–S7 and A-B2/A-B3 in `internal/server` (`go test ./...` passes; `gofmt -l .` and `go vet ./...` clean). Manual smoke test: ran the real binary, redeemed the launch code via curl and via the browser pane, confirmed `/api/health` and the static page serve correctly, confirmed a second instance against the same data directory is refused by the lock file, confirmed the original listener is undisturbed when the configured port is already in use.

**Discoveries:**
- `go.mod`'s `go` directive is pinned to the exact installed toolchain version (`go 1.26.2`) by `go mod init`; left as-is rather than hand-trimming to `go 1.26`, since Go treats a full version as a toolchain requirement, not a behavior change.
- The frontend embed (`//go:embed`) must live in a Go file *inside* `frontend/` (package `frontend`), not in `internal/server`, because embed patterns are resolved relative to the source file's own directory. `internal/server` imports the `frontend` package's `embed.FS` instead of embedding the files itself.
- `GET /launch` returns a 303 redirect to `/`. In tests the server has no frontend configured (`Config.Frontend` is nil), so following that redirect 404s — not a routing bug. `servertest.Start` now uses a client with `CheckRedirect: http.ErrUseLastResponse` and asserts 303 directly, rather than following the redirect.
- Confirmed Go 1.22+ `ServeMux` method-prefixed patterns (`"GET /launch"`) work as expected with the middleware chain wrapping the mux, since middleware wraps the mux pointer, not a snapshot — routes registered after `server.New` (e.g. `/api/health`, added by `main` after construction) still resolve correctly.

**Deviations from the plan file:** none. Rules S1–S7 and A-B2/A-B3 behave exactly as specified in `SECURITY-MODEL.md` and `PHASE1-ACCEPTANCE.md`.

**Not done here (by design, scoped to step 1):** storage, endpoint/credential/egress, provider adapter, generation pipeline, real frontend views, and the live/manual sign-off tests all remain for steps 2–7 of `docs/PHASE1-PLAN.md`.

## 2026-10-17 — Phase 1 step 2: storage

**Built:** `modernc.org/sqlite` added at v1.60.1 (BSD-3-Clause, confirmed from the vendored module's `LICENSE` file; `ARCHITECTURE.md` section 8 flipped from "Planned" to "Added"). `migrations/` (new top-level package, `embed.go` + `0001_init.sql`: `endpoints`, `endpoint_credentials`, `models`, `conversations`, `messages`, `generations`, plus supporting indexes). `internal/store`: `store.go` (`Open` sets WAL, `foreign_keys=ON`, `busy_timeout=5000`, a single max-open-connection since SQLite serializes writes anyway); `migrate.go` (parses `NNNN_*.sql` filenames for version order, applies each migration in its own transaction, refuses and leaves the file untouched if `PRAGMA user_version` is already past the newest embedded migration); `time.go` (shared RFC3339Nano timestamp codec); one typed query file per table (`endpoints.go`, `credentials.go`, `models.go`, `conversations.go`, `messages.go`, `generations.go`); `backup.go` (`VACUUM INTO` a timestamped file under `backups/`, then opens that copy on its own connection to `DELETE FROM endpoint_credentials` and `VACUUM` it, per DR-016); `storetest/` (mirrors `servertest`: `New`/`NewWithDir` give a fresh migrated database in a temp dir). `internal/api/backup.go` (`BackupHandler`) wired into `cmd/labbonet/main.go` as `POST /api/backup`, opening the store at `<data-dir>/labbonet.db` alongside the existing lock and session manager.

**Tests:** `internal/store/migrate_test.go` (A-B5 fresh start + restart keeps data and `user_version`; A-B6 migrate from an empty database, reopening an already-migrated database is a no-op; A-B7 refuses `user_version=999`, confirmed byte-identical file before/after); `internal/store/backup_test.go` (A-B8: restore by copying the backup file over the live database gives an identical conversation and message set, with `HasCredential` false in the restored copy); one query test file per table covering create/get/list/update/delete and the cascade deletes (`ON DELETE CASCADE` for credentials, models, messages, generations) plus A-E6 (same model ID on two endpoints stays two rows) and A-C8/A-C11 (reported vs. requested model, full provenance fields). `internal/api/backup_test.go` exercises `BackupHandler` directly against a `storetest` store. `gofmt -l .`, `go vet ./...`, `go test ./...`, `go build ./cmd/labbonet` and `CGO_ENABLED=0 go build ./...` all pass.

**Discoveries:**
- `modernc.org/sqlite`'s `database/sql` driver executes a multi-statement string passed to a single `Exec` call statement-by-statement (confirmed empirically: `0001_init.sql`'s six `CREATE TABLE` + four `CREATE INDEX` statements all apply from one `tx.Exec(rawFileContents)`), so `migrate.go` only strips comments/blank lines and splits on top-level `;` for clarity, with no need for a driver-specific workaround.
- `PRAGMA user_version = N` can be executed inside the same transaction as the schema statements it follows; committing both together is what makes "apply in one transaction" and "track by user_version" the same step, not two.
- Restoring a `VACUUM INTO` backup over the live file needs the original's `-wal`/`-shm` siblings removed first (the test does this), otherwise SQLite's WAL recovery on next open can reapply stale frames from the old file instead of reading the restored one directly.
- Chose `INTEGER PRIMARY KEY AUTOINCREMENT` IDs for every table except `models` (natural composite key `(endpoint_id, model_id)`, per "model identity is always the pair") and `endpoint_credentials` (one-to-one on `endpoint_id`). `ARCHITECTURE.md` section 5 lists key columns but not ID types; this is an implementation default in the same spirit as section 1, not a new entry there, since nothing outside this step depends on the choice yet.

**Deviations from the plan file:** none in scope. The "migration test from an empty database and from 0001" in `PHASE1-PLAN.md` step 2 is, as of this step, the same test twice over (there is only one migration, so "from 0001" and "from empty" are the same starting point); `TestMigrate_FromEmptyDatabase` notes this and also re-opens an already-migrated database to prove re-migration is a no-op, which is the part of that requirement that will matter once a `0002_*.sql` exists.

**Not done here (by design, scoped to step 2):** endpoints/credentials CRUD API, egress client, provider adapter, generation pipeline, frontend, and the live/manual sign-off tests remain for steps 3–7.
