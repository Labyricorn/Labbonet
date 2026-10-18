# Phase 1 Implementation Plan

**Version:** 0.1 (2026-10-18)
**Status:** Draft for owner approval.
**Goal:** reach Phase 1 sign-off in `PHASE1-ACCEPTANCE.md` in seven steps, each one reviewable on its own.
**Design sources:** `ARCHITECTURE.md` (what to build), `SECURITY-MODEL.md` (controls and S-tests), `PROVIDER-SPEC.md` (adapter contract), `PHASE1-ACCEPTANCE.md` (A-tests).

---

## 1. Working rules (all steps)

- **One step, one branch, one PR.** Branch names `phase1/<n>-<slug>`, for example `phase1/1-skeleton`. Steps can be split into smaller PRs, but are not merged out of order unless the dependency table (section 3) allows it.
- **Tests come with the code.** Each step lands with its listed tests passing. A step is not done because the code exists; it is done when its done criteria hold.
- **Verification on every PR:** `gofmt -l .` prints nothing; `go vet ./...` is clean; `go test ./...` passes; `go build ./cmd/labbonet` succeeds.
- **Dependencies:** add a module only if `ARCHITECTURE.md` section 8 lists it, and confirm its license at the pinned version in the same PR. Anything new needs an inventory row first.
- **GobboNet reuse:** only from the pinned commit, with a `REUSE-LOG.md` row in the same PR (`GOBBONET-REFERENCE.md`).
- **Security controls are not deferred.** A step that adds an `/api` route or an outbound call ships it behind the controls from steps 1 and 3; no temporary bypass flags.
- **Test hosts:** automated tests use in-process fake servers (`httptest`), never the real Strata or LM Studio. Live tests are run by hand and recorded in the PR.
- **DOX:** each PR updates the owning doc when a contract changes and adds a devlog entry.

## 2. Test infrastructure

Built in the step that first needs it, then shared:

| Helper | Package | First step | Purpose |
|---|---|---|---|
| Test server and client | `internal/server/servertest` | 1 | Starts the backend on a random loopback port with a temp data dir; returns a client with a valid session |
| Temp store | `internal/store/storetest` | 2 | Fresh migrated database per test |
| Fake provider | `internal/providers/fakeprovider` | 4 | Scriptable OpenAI-compatible server: model lists (Strata and LM Studio shapes), SSE streams with keep-alives, `usage`/`timings`, mid-stream drop, slow first byte, error statuses, redirects, header capture |
| Render test page | `frontend/tests/render.html` | 6 | In-browser checks of the Markdown and sanitizer pipeline; opened by hand, no Node needed |

## 3. Steps and dependencies

| Step | Builds on | Can run in parallel with |
|---|---|---|
| 1 Skeleton and security shell | none | none |
| 2 Storage | 1 | 3 (in separate packages) |
| 3 Endpoints, credentials and egress | 1, 2 for persistence | 2 for egress and secrets only |
| 4 OpenAI adapter | 3 (egress client) | 5 interface work |
| 5 Generation pipeline | 2, 3, 4 | 6 (against a stubbed API) |
| 6 Frontend | 1 to 5 APIs | none |
| 7 Live run and sign-off | 1 to 6 | none |

---

## Step 1: Skeleton and security shell

**Scope:** a runnable binary that serves a placeholder page on loopback with every browser-to-backend control from `SECURITY-MODEL.md` section 4 already in place.

**Files:**
- `go.mod` (module `github.com/Labyricorn/Labbonet`, `go 1.26`)
- `cmd/labbonet/main.go`: flags `--port` (default 4815), `--host` (only `127.0.0.1` accepted), `--data-dir`, `--frontend-dir`, `--open`; graceful shutdown on Ctrl+C
- `internal/server/server.go`: listener, router, static files via `go:embed`
- `internal/server/middleware.go`: Host allowlist (421), Origin and `Sec-Fetch-Site` check (403), `X-Labbonet: 1` check (403), session cookie check (401), security headers and CSP
- `internal/server/session.go`: 256-bit token, single-use launch code expiring after 2 minutes, `/launch?code=` sets the cookie and redirects
- `internal/server/lock.go`: data-dir lock file
- `internal/api/health.go`: `GET /api/health`
- `frontend/index.html`, `frontend/app.js`, `frontend/app.css`: placeholder shell that calls `/api/health`
- `internal/server/servertest/`: shared test helper

**Tests:** S1 to S7; A-B1 (manual), A-B2, A-B3; unit tests for each middleware rule.

**Done when:**
- `labbonet` starts as a standard user, prints the launch URL, and the page shows backend health.
- S1 to S7, A-B2 and A-B3 pass in `go test`.
- No `Access-Control-Allow-*` header appears on any response (checked in tests).
- `--host 0.0.0.0` refuses to start.

## Step 2: Storage

**Scope:** the authoritative SQLite database with versioned migrations and backup (DR-006).

**Files:**
- `internal/store/store.go`: open with WAL, `foreign_keys=ON`, `busy_timeout`; close on shutdown
- `internal/store/migrate.go`: apply embedded migrations in one transaction each; track `PRAGMA user_version`; refuse a newer database
- `migrations/0001_init.sql`: tables from `ARCHITECTURE.md` section 5 (`endpoints`, `endpoint_credentials`, `models`, `conversations`, `messages`, `generations`)
- `internal/store/{endpoints,conversations,messages,generations}.go`: typed queries
- `internal/store/backup.go`: `VACUUM INTO` into `backups/`, then empty `endpoint_credentials` in the copy
- `internal/api/backup.go`: `POST /api/backup`
- `internal/store/storetest/`
- `ARCHITECTURE.md` section 8: confirm the `modernc.org/sqlite` license

**Tests:** A-B5 to A-B8; query tests for each table; a migration test from an empty database and from 0001.

**Done when:**
- Data survives a restart; a database newer than the binary is refused untouched.
- A backup restores to an identical conversation set, with no credential rows.
- No cgo: `CGO_ENABLED=0 go build ./...` succeeds.

## Step 3: Endpoints, credentials and egress

**Scope:** registering endpoints, storing credentials safely, and the only outbound HTTP client (`SECURITY-MODEL.md` section 5).

**Files:**
- `internal/secrets/secrets.go`: `Protector` interface
- `internal/secrets/dpapi_windows.go`: DPAPI current-user protect/unprotect via `golang.org/x/sys/windows`
- `internal/secrets/unsupported_other.go`: returns "credentials unsupported on this platform" (no plaintext fallback)
- `internal/endpoints/registry.go`: validate base URL (scheme, no userinfo, no path tricks), derive a suggested privacy class, require acknowledgement for `external`, refuse metadata addresses
- `internal/egress/client.go`: dial guard by privacy class, no redirects, timeouts from `PROVIDER-SPEC.md` 1.4, response size limits, `Authorization` attached only for the endpoint's own credential
- `internal/egress/paths.go`: the fixed path allowlist from `PROVIDER-SPEC.md` 1.5
- `internal/api/endpoints.go`: endpoint CRUD and `PUT`/`DELETE .../credential` (write-only)
- Log redaction helper used by the server logger
- `ARCHITECTURE.md` section 8: confirm the `golang.org/x/sys` license

**Tests:** S8 to S13; A-E1 (live registration of both hosts), A-E7; dial-guard table tests (loopback, RFC 1918, public, metadata, DNS changing class); a request to any path outside the allowlist fails.

**Done when:**
- Both real endpoints can be registered with classes `local` and `lan`.
- No code path outside `internal/egress` makes an outbound HTTP request (checked by a test that greps for `http.Get`, `http.Client{` and `net.Dial` outside the package).
- Secrets never appear in API responses, logs or backups (S12, S13).

## Step 4: OpenAI adapter

**Scope:** the one provider adapter, with Strata and LM Studio detection, model discovery and streaming chat (`PROVIDER-SPEC.md` sections 1 to 3).

**Files:**
- `internal/providers/provider.go`: `Provider`, `Model`, `ChatRequest`, `Event`, `Result`, error kinds
- `internal/providers/openai/detect.go`: `/health`, then `/api/v0/models`, then `/v1/models`
- `internal/providers/openai/models.go`: normalize both model-list shapes into the capability profile; hide `embeddings` models from chat
- `internal/providers/openai/sse.go`: SSE parser (keep-alive comments, `[DONE]`, late `usage`/`timings`, null content on `length`)
- `internal/providers/openai/chat.go`: request mapping, per-provider extras allowlist (never `strata_mcp`, never `ttl`), reported-model capture
- `internal/providers/openai/errors.go`: HTTP status and body mapping to the error taxonomy
- `internal/providers/fakeprovider/`
- `internal/api/models.go`: `POST .../test`, `POST .../models/refresh`, `GET .../models`

**Tests:** A-E2, A-E3 (against fakes with Strata and LM Studio shapes, then a live read-only check), A-E5, A-E6, A-C6, A-C7, A-C8; SSE parser table tests built from the observed Strata samples in `Strata_Protocol.md` 5.3.

**Done when:**
- Against the real hosts, the connection test detects Strata and LM Studio correctly and model refresh lists their models with type, state and context. These are read-only calls only.
- Every error kind in `PROVIDER-SPEC.md` 1.3 has a test that produces it.

## Step 5: Generation pipeline

**Scope:** conversations, the generate SSE stream, cancellation, concurrency and provenance (`ARCHITECTURE.md` 4.2).

**Files:**
- `internal/api/conversations.go`: conversation CRUD, model switch keeps history
- `internal/generate/runner.go`: persist user message, create assistant message and `generations` row, acquire endpoint slot, run adapter, flush at most once per second, set final status
- `internal/generate/slots.go`: per-endpoint semaphore from `concurrency_limit`
- `internal/generate/registry.go`: running generations by ID for cancel
- `internal/api/generate.go`: `POST /api/conversations/{id}/generate` (SSE out), `POST /api/generations/{id}/cancel`
- `internal/api/diagnostics.go`: `GET /api/diagnostics` (metadata only)
- Guard: refuse to send to a model in state `not-loaded` unless the request carries `confirm_load: true` (server side, so the UI cannot skip it)

**Tests:** A-C4 (fake), A-C5, A-C9 (fake slot contention), A-C11, A-D2, S15 (server side); a test that a model switch mid-conversation keeps every earlier message and its provenance.

**Done when:**
- Through the API alone (curl or test client), a conversation can stream from the fake provider, be cancelled with partial text saved, and switch models without losing history.
- Ctrl+C during a stream saves the partial reply as `cancelled` (A-B4).

## Step 6: Frontend

**Scope:** the Phase 1 UI on top of the step 1 to 5 APIs (`ARCHITECTURE.md` section 3).

**Files:**
- `frontend/index.html`, `frontend/app.css`
- `frontend/js/api.js`: fetch wrapper (adds `X-Labbonet`, handles 401 by showing "reopen the launch URL"), SSE reader over `fetch`
- `frontend/js/conversations.js`, `frontend/js/chat.js`: list, thread, streaming output, stop button, collapsible reasoning
- `frontend/js/endpoints.js`: register, test, credential set/clear, privacy class and plain-HTTP warning
- `frontend/js/models.js`: selector grouped by endpoint, state and context shown, embeddings hidden, `not-loaded` confirmation dialog naming the model and the auto-evict risk
- `frontend/js/diagnostics.js`
- `frontend/js/render.js`: Markdown with raw HTML off, then sanitize; safe links
- `frontend/vendor/` and `frontend/vendor/VENDOR.md`: pinned Markdown renderer and sanitizer with SHA-256 and license
- `frontend/tests/render.html`

**Tests:** S14 (render test page plus a manual check in the browser), A-E4, A-C10, A-D1 (manual walkthroughs, written up in the PR with screenshots); CSP violation check: the browser console shows no CSP errors during the walkthrough.

**Done when:**
- Every Phase 1 user action works from the UI without curl.
- The render test page passes and the console is clean.
- No inline script or style; CSP unchanged from `SECURITY-MODEL.md` section 4.

## Step 7: Live run and sign-off

**Scope:** run the live and manual tests against the real hosts and record the results.

**Owner prerequisites (`PHASE1-ACCEPTANCE.md` section 3):** P1 Strata running; P2 an LM Studio chat model loaded on `10.138.11.11:1234`; P3 and P4 short auth windows; P5 a Strata cold start.

**Work:**
- Run A-B1, A-B4, A-C1, A-C2, **A-C3 (exit scenario)**, A-C9, A-ST5 to A-ST7, A-LM6, A-LM7, S16, S17.
- Fill in the LM Studio stream findings (reasoning field, `usage`, `stats`) in `PROVIDER-SPEC.md` section 5 and close the open items in section 7.
- Tick the DR-002, DR-005, DR-006, DR-012 and DR-015 acceptance criteria in `decision_register.md` with links to evidence; add a log row.
- Fill in the sign-off table in `PHASE1-ACCEPTANCE.md` section 6.

**Done when:** the owner signs off Phase 1, with any waivers recorded.

---

## 4. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| Strata is shared with another client and has one slot | Live tests wait or time out | Run live tests when the other client is idle; `/status` shows the queue |
| No LM Studio model loaded | LM Studio live tests blocked | Owner loads one before step 7 (P2); steps 1 to 6 do not need it |
| JIT load triggered by mistake | A model loads, another client's model may be evicted | Server-side `confirm_load` guard in step 5; probes check state first |
| DPAPI is Windows-only | Credentials do not work elsewhere | Accepted for Phase 1; the `Protector` interface leaves room |
| SSE over `fetch` behaves differently across browsers | Streaming glitches | Test in the owner's main browser first; record which browsers were checked |
| Plain-HTTP LAN endpoint | Traffic readable on the LAN | Classified `lan`, UI warning; owner recommendation in `SECURITY-MODEL.md` 5.2 |
