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
