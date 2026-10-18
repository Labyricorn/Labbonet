# Labbonet Architecture

**Version:** 0.1 (2026-10-18)
**Status:** Phase 1 design. Implements DR-002, DR-006, DR-012 and DR-015; security detail in `SECURITY-MODEL.md`, provider detail in `PROVIDER-SPEC.md`.

---

## 1. Implementation defaults

The owner approved these defaults on 2026-10-18. They are implementation choices, not decision-register entries; changing one needs a note here and in the devlog, not a DR amendment.

| Choice | Default | Reason |
|---|---|---|
| Standalone HTML proof of concept | Skipped. Phase 1 starts with the minimal Go backend. | Strata has CORS off (`Strata_Protocol.md` section 8), so a standalone page cannot reach it. |
| Frontend tooling | Plain ES modules, no build step, no Node at runtime | Small, auditable; matches the "no hidden execution" principle. |
| Frontend dependencies | Vendored into `frontend/vendor/` at pinned versions with SHA-256 and license recorded | No CDN loads at runtime (proposal section 15.7). |
| SQLite driver | `modernc.org/sqlite` (pure Go) | No C toolchain on the build machine (gcc absent); cgo drivers are out. |
| Credential protection | Windows DPAPI, current-user scope, via `golang.org/x/sys/windows` | OS-backed, no new dependency. Behind an interface for other platforms later. |
| Backend bind | `127.0.0.1:4815`, port configurable | Loopback only (DR-010 A). Avoids Strata 8080, LM Studio 1234, Ollama 11434. |
| Backend API protection | Host allowlist, Origin check, per-launch session token | See `SECURITY-MODEL.md` section 4. |
| Docs layout | New docs in `docs/`; existing root documents stay where they are | Avoids link churn. |

## 2. Components

```text
 Browser (same machine)                          Labbonet backend (Go, 127.0.0.1:4815)
┌──────────────────────────────┐   HTTP+SSE   ┌──────────────────────────────────────────┐
│ frontend/ (ES modules)       │─────────────>│ server: static files, CSP, Host/Origin,  │
│  chat view, endpoint manager │  same origin │         session middleware               │
│  model selector, diagnostics │<─────────────│ api:    endpoints, models, conversations,│
│                              │              │         generations, diagnostics, backup │
│ [Phase 2] WebLLM worker      │              │ store:  SQLite + migrations              │
│  (inference stays in-browser)│              │ secrets: DPAPI-protected credentials     │
└──────────────────────────────┘              │ providers: adapter registry              │
                                              │ egress: allowlisted HTTP client          │
                                              └───────────────┬──────────────────────────┘
                                                              │ only registered endpoints
                                 ┌────────────────────────────┼────────────────────────┐
                                 v                            v                        v
                       Strata 127.0.0.1:8080      LM Studio 10.138.11.11:1234     (later) Ollama,
                       (local machine)            (trusted LAN, plain HTTP)       llama.cpp, gateways
```

Boundaries:

- **Browser to backend:** the only channel. The browser never contacts an inference endpoint directly in Phase 1, never receives a provider credential, and never supplies a URL that the backend fetches.
- **Backend to providers:** only through the egress client, only to registered endpoints, only on the fixed request paths in `PROVIDER-SPEC.md`.
- **WebLLM (Phase 2):** inference runs in a browser Web Worker. The backend stores the resulting messages but never proxies WebLLM inference.

## 3. Frontend

- Served by the backend from files embedded with `go:embed` (the binary is self-contained). A `--frontend-dir` development flag serves from disk instead.
- Plain HTML, CSS and ES modules. No framework, no bundler.
- Markdown rendering with raw HTML disabled, followed by sanitization (candidates: `markdown-it` MIT with `html: false`, and `DOMPurify` Apache-2.0/MPL-2.0). Final choice and versions are recorded in `frontend/vendor/VENDOR.md` when added.
- Phase 1 views: conversation list, chat thread with streaming output and a stop button, endpoint manager (register, test, credential set/clear), model selector grouped by endpoint (proposal section 18), diagnostics panel.
- Reasoning content (`reasoning_content`) is shown in a collapsible block, separate from the answer.

## 4. Backend

Go 1.26, single binary `labbonet`, standard library `net/http` routing.

| Package | Responsibility |
|---|---|
| `cmd/labbonet` | Flags, startup, shutdown, launch URL |
| `internal/server` | Listener, middleware (Host allowlist, Origin, session, security headers), static files |
| `internal/api` | JSON handlers and SSE generation stream |
| `internal/store` | SQLite access, embedded migrations, backup |
| `internal/secrets` | Credential protect/unprotect (DPAPI on Windows) |
| `internal/endpoints` | Endpoint registry, privacy classification, egress policy |
| `internal/egress` | The only outbound HTTP client: dial guard, no redirects, size limits, timeouts |
| `internal/providers` | Adapter interface and the `openai` adapter with Strata and LM Studio detection hints |

### 4.1 API surface (Phase 1)

All paths are under `/api`. Every request needs the session cookie and the `X-Labbonet: 1` header (see `SECURITY-MODEL.md`).

| Method and path | Purpose |
|---|---|
| `GET /api/health` | Backend status and version |
| `GET /api/endpoints`, `POST /api/endpoints` | List, register |
| `PATCH /api/endpoints/{id}`, `DELETE /api/endpoints/{id}` | Edit, remove (removes its credential) |
| `POST /api/endpoints/{id}/test` | Connection test and provider detection |
| `POST /api/endpoints/{id}/models/refresh`, `GET /api/endpoints/{id}/models` | Model discovery, cached inventory |
| `PUT /api/endpoints/{id}/credential`, `DELETE /api/endpoints/{id}/credential` | Write-only credential set, clear |
| `GET /api/conversations`, `POST /api/conversations` | List, create |
| `GET /api/conversations/{id}`, `PATCH`, `DELETE` | Read with messages, rename or change model, delete |
| `POST /api/conversations/{id}/generate` | Append user message and stream the reply (SSE) |
| `POST /api/generations/{id}/cancel` | Cancel a running generation |
| `GET /api/diagnostics` | Recent requests: endpoint, model, status, timings (no message content) |
| `POST /api/backup` | Write a consistent database backup into the data directory |

### 4.2 Generation flow

1. Persist the user message.
2. Create the assistant message with status `streaming` and a `generations` row (provenance).
3. Acquire the endpoint's concurrency slot (default 1; Strata runs one request at a time).
4. Call the adapter; relay normalized events to the browser over SSE.
5. Accumulate content and flush it to the database at most once per second.
6. Finish with status `complete`, `cancelled`, `interrupted` (browser disconnected) or `error`. Partial text is always kept.

In Phase 1 the generation is tied to the browser request: if the browser disconnects, the backend cancels upstream and marks the message `interrupted`. Reattaching to a running generation is a later enhancement.

## 5. Storage (DR-006)

- **Data directory:** `%LOCALAPPDATA%\Labbonet` (override with `--data-dir`). Contents: `labbonet.db`, `backups/`, `logs/`, `labbonet.lock`.
- The application deletes only files it created inside this directory. It has no uninstall routine.
- SQLite settings: WAL journal, `foreign_keys=ON`, `busy_timeout`.
- **Schema versioning:** numbered SQL migrations embedded in the binary, applied in order inside a transaction, tracked by `PRAGMA user_version`. A database newer than the binary is refused, not modified.
- **Backup:** `VACUUM INTO` a timestamped file in `backups/`. Credentials are excluded (DR-016); the backup copy has the credential table emptied.

Phase 1 tables (outline):

| Table | Key columns |
|---|---|
| `endpoints` | id, name, provider_type, base_url, privacy_class, enabled, concurrency_limit, detected_service, created_at |
| `endpoint_credentials` | endpoint_id, protected_blob, created_at |
| `models` | endpoint_id, model_id, type, state, context_length, capabilities_json, seen_at |
| `conversations` | id, title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at |
| `messages` | id, conversation_id, parent_id (for later branching), role, content, reasoning, status, created_at |
| `generations` | id, message_id, endpoint_id, model_requested, model_reported, params_json, started_at, first_token_at, finished_at, finish_reason, usage_json, timings_json, error_kind |

Model identity is always the pair (endpoint_id, model_id) (proposal section 8.1).

## 6. Startup and shutdown

- Runs as the normal user. No admin rights, service install, autostart, firewall change or antivirus exclusion.
- Binds `127.0.0.1` only. Phase 1 refuses a non-loopback `--host` value outright.
- If the port is busy, it exits with a clear message. It never stops another process.
- A lock file in the data directory prevents two instances sharing one database.
- On start it prints a one-time launch URL (section 4 of `SECURITY-MODEL.md`). `--open` additionally opens the default browser; off by default.
- Ctrl+C: stop accepting requests, cancel running generations (partial text saved), close the database.

## 7. Repository layout

```text
Labbonet/
├── cmd/labbonet/          main package
├── internal/              backend packages (section 4)
├── frontend/              HTML, CSS, ES modules; vendor/ with VENDOR.md
├── migrations/            numbered .sql files (embedded)
├── tests/                 integration and negative security tests
├── docs/                  design documents
├── go.mod, go.sum
└── (root) README, AGENTS, LICENSE, proposal, decision register, Strata protocol
```

The Go module path is `github.com/Labyricorn/Labbonet`.

## 8. Dependency inventory (DR-004)

Dependencies must be AGPLv3-compatible. Record each one here when it is added.

| Dependency | Use | License | Status |
|---|---|---|---|
| Go standard library | Everything not listed | BSD-3-Clause | Required |
| `modernc.org/sqlite` | SQLite driver | BSD-3-Clause | Added (Phase 1 step 2, v1.60.1; license confirmed from the vendored module's `LICENSE` file) |
| `golang.org/x/sys` | DPAPI on Windows | BSD-3-Clause | Added (Phase 1 step 3, v0.48.0; license confirmed from the vendored module's `LICENSE` file; it was already present as an indirect dependency of `modernc.org/sqlite` and is now also a direct one) |
| `markdown-it` | Markdown rendering | MIT | Candidate (Phase 1) |
| `DOMPurify` | HTML sanitization | Apache-2.0 or MPL-2.0 | Candidate (Phase 1) |
| `@mlc-ai/web-llm` | Browser inference | Apache-2.0 | Planned (Phase 2) |

Licenses above are from general knowledge and must be confirmed from each package's own license file, at the pinned version, when it is added.
