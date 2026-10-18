# Labbonet Architecture

**Version:** 0.1 (2026-10-18)
**Status:** Phase 1 design. Implements DR-002, DR-006, DR-012 and DR-015; security detail in `SECURITY-MODEL.md`, provider detail in `PROVIDER-SPEC.md`.

---

## 1. Implementation defaults

Current application development version: **V0-2-0** (owner, 2026-10-18); `/api/health` reports that label. Document version numbers above are document revisions. Remaining Phase 1 provider auth/cold-start checks and final Phase 1 sign-off are deferred until **V2-0-0**; current GobboNet feature/style parity work continues.

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
- Markdown rendering with raw HTML disabled (`markdown-it` with `html: false`), followed by DOMPurify sanitization. Pinned versions, licenses and hashes are in `frontend/vendor/VENDOR.md`.
- Phase 1 views: conversation list, chat thread with streaming output and a stop button, endpoint manager (register, test, credential set/clear), model selector grouped by endpoint (proposal section 18), diagnostics panel.
- Reasoning content (`reasoning_content`) is shown in a collapsible block, separate from the answer.
- Owner visual requirement: match the pinned GobboNet interface pixel for pixel except for model dynamics. Presentation files in `frontend/css/` are selectively adapted from the pinned source with notices and entries in `REUSE-LOG.md`; `app.css` contains Labbonet integration rules. Upstream HTML presentation structures are adapted in the shell/chat/cast modules; JavaScript runtime logic is Labbonet work. No upstream JS runtime, inline handlers/styles or excluded behaviors are imported. Authorship boundaries are recorded in `REUSE-LOG.md`, pinned source hashes/spans in `GOBBONET-PROVENANCE.json`, and public credit in root `NOTICE.md`. The pinned tree omits its referenced Atkinson font file, so its original Cascadia/Consolas fallback stack is used.
- The reference sidebar retains its footer positions: CONFIG opens endpoint controls (including Diagnostics), DATA creates a credential-free backup, ABOUT shows attribution and a Diagnostics entry. The in-progress parity milestone 1 adds a working CHARS library/editor and per-thread cast; extensions and scheduling remain disabled. Visual acceptance is separate from functional verification and awaits owner review.

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
| `GET /api/conversations/{id}`, `PATCH`, `DELETE` | Read (visible branch with variant info and tree revision), rename or change model, delete |
| `POST /api/conversations/{id}/generate` | Append a user message (optional text `attachments`), or with `regenerate_parent_id` add a reply variant, start the reply as a detached job and stream it (SSE); optional `revision`, `max_reply_chars`, `reasoning_limit_chars` |
| `POST .../messages/{mid}/edit`, `.../activate`, `.../fork`; `DELETE .../messages/{mid}` | Milestone 3 tree edits (section 11); edit/activate/delete require the tree `revision` |
| `GET /api/conversations/search?q=`, `PUT /api/conversations/order`, `PUT /api/conversations/{id}/organize` | Content search, manual order, pin/folder/tags (section 11) |
| `GET/POST /api/folders`, `PATCH/DELETE /api/folders/{id}` | Sidebar folders |
| `GET /api/generations/{id}/events` | Re-attach to a running or just-finished reply: SSE `snapshot` (text so far), live events, `end` (stored status); 404 once the job is no longer held |
| `GET /api/conversations/{id}/generation` | `{generation_id, active}` for the conversation's running reply |
| `POST /api/generations/{id}/cancel` | Cancel a running generation |
| `GET/PUT /api/memory/settings`, `POST /api/memory/storybook/preview`, `GET/DELETE /api/memory/embeddings` | Milestone 5 (section 13): revisioned memory settings, storybook parse preview, vector cache stats and clear |
| `GET /api/conversations/{id}/memory`, `PUT .../summary`, `POST .../summary/compress` | Effective summary, revision history and retrieval records; hand edit/clear; explicit summarise-now |
| `GET/POST /api/macros`, `PUT/DELETE /api/macros/{id}`, `POST /api/macros/preview` | Milestone 6 (section 14): user macros (revisioned), expansion preview |
| `GET/PUT /api/appearance` | Milestone 6: active appearance preset and custom presets (revisioned) |
| `GET/PUT /api/search/settings`, `PUT/DELETE /api/search/credential`, `POST /api/search/test`, `GET /api/conversations/{id}/search` | Milestone 6: opt-in web search settings, write-only key, explicit connection test, per-reply search records |
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
| `conversations` | id, title, endpoint_id, model_id, system_prompt, params_json, created_at, updated_at; schema 3 adds active_leaf_id, revision, pinned, folder_id, position, forked_from_conversation_id/message_id |
| `messages` | id, conversation_id, parent_id (tree link), role, content, reasoning, status, created_at, speaker_json; schema 3 adds selected_child_id, origin, origin_id |
| `folders`, `conversation_tags` | schema 3: sidebar folders (unique ignoring case); per-thread tags |
| `memory_settings`, `conversation_summaries`, `embedding_cache`, `retrieval_records` | schema 4 (section 13): revisioned retrieval/summary/embedding settings, append-only summary revisions, content-and-model-keyed vectors, bounded retrieval telemetry |
| `search_settings`, `search_records`, `macros`, `appearance_settings` | schema 5 (section 14): revisioned search settings plus the protected search key, bounded per-reply search records, user macros, appearance selection and custom presets |
| `schedules`, `schedule_runs`, `profiles`, `profile_state` | schema 6 (section 15): timezone-aware prompts, bounded run ledger, revisioned settings bundles and last-applied profile |
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
| `markdown-it` | Markdown rendering | MIT | Added (Phase 1 step 6, v14.1.0; vendored UMD build, license confirmed from the tagged `LICENSE` file; see `frontend/vendor/VENDOR.md`) |
| `DOMPurify` | HTML sanitization | Apache-2.0 or MPL-2.0 | Added (Phase 1 step 6, v3.2.3; vendored UMD build, license confirmed from the tagged `LICENSE` file; see `frontend/vendor/VENDOR.md`) |
| `@mlc-ai/web-llm` | Browser inference | Apache-2.0 | Planned (Phase 2) |

Licenses above are from general knowledge and must be confirmed from each package's own license file, at the pinned version, when it is added.

## 9. Cast and prompt foundation (parity milestone 1, implementation in progress)

- Migration `0002_cast.sql` adds revisioned `cast_cards` (typed inert JSON), `cast_settings` defaults, per-conversation `conversation_cast`, and immutable message `speaker_json`. Existing conversations initialize without cast; defaults are frozen atomically for new conversations. Card deletion clears active references and advances selection revisions, preserving historical speakers.
- API: `GET/POST /api/cast`, `PUT/DELETE /api/cast/{id}`, `PUT /api/cast/order` (complete kind order with ID/revision pairs, atomic), `GET/PUT /api/cast/defaults`, `GET/PUT /api/conversations/{id}/cast`, `POST /api/conversations/{id}/greeting` (expected selection revision). All use existing middleware. JSON is strict and bounded to 4 MiB; stale mutations return 409. Greeting insertion is explicit, revision checked and empty-conversation-only.
- `internal/cast` validates card kinds, text/list/macro bounds, supported numeric controls and local PNG/JPEG assets. Full decoding follows bounded header/dimension checks (1 MiB decoded compressed file, 4096 per dimension / 16 MP). Remote URLs, SVG and executable hooks are rejected.
- `internal/prompt` performs bounded inert template expansion and resolved request snapshots. Character macros override persona macros; reserved built-ins cannot be assigned. Unknown macros remain inert visible text. Carousel choice/index, resolved messages, model/card limits, supported temperature/top_p and timestamps are recorded. Prompt snapshots exclude image assets and unused greetings; message speaker snapshots retain portraits/names/colors for durable historical display.
- Context fallback is **8192** when inventory has no verified context length; character limits may reduce that window. Reply fallback is **256** tokens; explicit request controls override card controls. Budgeting estimates UTF-8 bytes plus 16/message, reserves reply space and trims whole old turns. This is a conservative client estimate, not verified tokenizer equivalence. Oversized system/current turns and invalid expansion/controls reject before any messages are written.
- Persona descriptions follow the pinned cadence: frequency zero disables description; otherwise first user turn and multiples of N, immediately before final user message. Names/portraits remain independent of description injection. This is separately credited behavior reference, implemented in Go.
- Atomic turn creation checks history count, cast revision and absence of a streaming assistant, then inserts both speakers and generation provenance. Conflicting same-thread submissions return 409. Endpoint slots still serialize different threads for a shared endpoint. Cancellation is registered before exposing generation ID to callbacks.
- Existing-schema upgrades first create a credential-free backup; live encrypted credentials, endpoints, conversations, messages and generation provenance remain intact. Tests cover populated v1 upgrades and migration rollback.
- Browser: CHARS has character/persona CRUD/copy/order/defaults, local-image file input/preview/remove, inert macro editor, greeting/carousel variants and supported presets. Per-thread selectors persist; speaker rendering uses historical snapshots. Validated hex colors use CSSOM, fixed CSS integration and safe DOM quote spans; no inline styles or CSP change. Unsupported samplers show a reason. Browser upload end-to-end and remaining editor acceptance are recorded in the handover; owner visual acceptance is open.

## 10. Card interchange and DATA (parity milestone 2)

- `internal/interchange` (no store dependency) parses untrusted uploads: Labbonet snapshot JSON, native card JSON, Tavern V1/V2/V3 JSON, Tavern PNG (`ccv3` preferred, `chara` V2 fallback) and CHARX / sharing ZIP (`card.json` plus an `embeded://` icon). Format is detected by magic bytes. Exports: native JSON, V3 JSON, V2 JSON, V3 PNG with V2 `chara` backfill, and sharing ZIP. Labbonet-only fields (kind, style, carousel, macros, limits, samplers, colors, background) round-trip through `extensions.labbonet`; standard fields come from the standard Tavern keys.
- Bounds: upload 64 MiB; card JSON 4 MiB; PNG chunks 8 MiB / 4096 chunks with tEXt CRC verification (zTXt/iTXt ignored, so no decompression of metadata); ZIP 64 entries, 8 MiB per entry, 24 MiB total, compression ratio and name checks. ZIPs with traversal/absolute/drive paths, symlinks, duplicates or executable extensions (.exe, .js, .ps1, .sh, .py, .wasm ...) are rejected outright; unknown assets are ignored with a report note. Embedded images are fully decoded, metadata-stripped by re-encoding and downscaled to the 1 MiB limit; remote icon URLs are never fetched.
- Unmapped metadata (creator notes, tags, unknown `extensions` including any script-like blocks) is stored only in `Card.Extra` (JSON object, 64 KiB) and re-emitted on export; it is never interpreted. Character-book entries merge into authored lore (keyword activation is not preserved; reported in the preview).
- Snapshot (`labbonet-snapshot` versions 1-2, scopes `full`, `cards`, `conversation`): cards, threads with messages (role, status, reasoning, saved speaker identity without local IDs) and cast references by export-local key, plus optional default cast. It never contains endpoints, credentials, models, provider settings, conversation parameters or generation provenance. Unknown fields, future versions, duplicate keys, bad references, roles, statuses and speakers are rejected before any write; `streaming` normalizes to `interrupted`. Imported threads have no endpoint/model and empty params.
- `store.ImportSnapshot` is one transaction; preview runs the same writes and rolls back. Policies: `add` (collisions renamed ` (imported)`) and `skip` (reuse same kind+name). Default cast applies only with the explicit option. `store.ResetData` scopes `conversations`, `cast`, `all`; endpoints and credentials are never touched.
- API: `POST /api/data/import?mode=preview|commit&policy=add|skip&defaults=0|1` (raw body), `GET /api/data/export?scope=full|cards|conversation&id=`, `GET /api/cast/{id}/export?format=native|v3|v2|png|zip`, `POST /api/data/reset` (`{scope, confirm:"RESET <SCOPE>"}`; a credential-free backup is written first and failure aborts the reset). All sit behind the existing middleware; downloads use `Content-Disposition: attachment` with sanitized names and `no-store`.
- Frontend: `frontend/js/data.js` provides the DATA dialog (backup, export, import preview/commit, typed-confirmation reset); per-card export menu and IMPORT CARDS in CHARS.
- Milestone 3 raised the snapshot to version 2 (version 1 still imports as one linear branch); see section 11.

## 11. Message tree, variants and thread organization (parity milestone 3)

- **Tree:** `messages.parent_id` links every message to the one it answers or varies; roots have none. Siblings are variants. An edit never rewrites text: it inserts a sibling (`origin=edit`, `origin_id` = the edited message) with the same speaker and makes it the visible one. A reroll is a new assistant sibling under the same user message (`origin=reroll`, `origin_id` = the reply it sits beside; the first reply to an edited user message is an ordinary reply). A fork copies the root-to-message chain into a new conversation (`origin=fork`, conversation `forked_from_*`; generation provenance stays with the source messages). `origin_id` and `forked_from_*` are provenance only, not foreign keys.
- **Visible branch:** `conversations.active_leaf_id` is the tip; `messages.selected_child_id` remembers which child continues each message, so switching a variant restores that variant's own continuation. Only the visible branch (`Store.ActivePath` / `PathTo`) is read by `generate.Runner`; other branches never reach a provider. Deleting a message removes it and its subtree (one recursive statement) and moves the branch to the previous or next sibling, else the parent. Migration 0003 turns legacy linear history (including earlier imports) into one branch.
- **Revisions:** `conversations.revision` increments on every tree change (turn, edit, delete, switch). Edit, delete and switch require the caller's revision (zero is rejected at the API); generate accepts an optional one. A mismatch is `409` and writes nothing. `StartTurn` also requires the expected tip (`active_leaf_id IS ?`) and no streaming message, replacing the old message-count check; tree edits are refused with `409` while any reply streams. Regeneration must name a user message of the same conversation.
- **Generation modes:** send (new user message below the tip plus assistant) and regenerate (`regenerate_parent_id`: assistant variant below an existing user message; prompt history is the path to that message).
- **Organization:** pin, folder, tags (<= 16, <= 32 characters, case-insensitive unique), manual `position` (`PUT /order` must list every conversation or conflicts; list order is pinned, position, newest). Folder delete unfiles its threads; resetting conversations also clears folders. Search is `LIKE` with escaped wildcards over title, tags and all branches' message text (ASCII case-insensitive; minimum 2 characters, 500 results).
- **Snapshot version 2:** messages carry `key`, `parent`, `selected` and `origin`/`origin_key`; conversations carry `active`, `pinned`, `folder` (by name, merged or created on import) and `tags`. `Validate` requires earlier-parent order (acyclic), unique keys, valid selections, known origins; provenance links outside the export are dropped. Version 1 snapshots build a linear chain.
- **Not implemented:** moving or reordering folders, bulk thread export, importing GobboNet's own snapshot format, and variants for the greeting root beyond edit. Replies run as detached jobs since milestone 4 (section 12).

## 12. Detached generation and chat tools (parity milestone 4)

- **Jobs:** `generate.Jobs.Start` runs `Runner.Run` on a context that is not the HTTP request's, returns once the turn is stored (or refused with the usual `409`/`400`/`502`), and keeps a `Job` per generation. Closing the tab, navigating or a dropped connection only detaches a reader; the reply continues and is stored. Explicit Stop (`POST /cancel` -> `Registry`) is the only user abort and works on a job still waiting for its endpoint slot (stored `cancelled`, provider never called). The `Registry`/`Slots` rules are unchanged: one running reply per conversation (`StartTurn` refuses a second with `409`, so no duplicate turn) and per-endpoint concurrency.
- **Replay:** a `Job` keeps the accumulated content/reasoning, not an event log, so memory is bounded by the reply. `Subscribe` atomically takes a `snapshot` and registers a 1024-update channel; a reader that falls behind is dropped (it re-subscribes for a fresh snapshot). Finished jobs stay for 2 minutes / 16 jobs, then `GET .../events` is `404` and the stored message is the record. Every stream ends with an `end` update carrying the stored status, error kind and finish reason.
- **Lifecycle:** shutdown calls `Registry.CancelAll`, then `Jobs.Wait` (5 s) so partial text is written before the database closes. At startup `Store.RecoverInterrupted` marks any message still `streaming` (a killed process) as `interrupted`, closes its generation with `error_kind=server_restart` and resubmits nothing.
- **Limits:** `max_reply_chars` (100 to 200000) ends the reply at the first sentence end at or after the cap, or `400` characters later at the latest; the stored and emitted text are the trimmed text, status `complete`, finish reason `reply_cap`. `reasoning_limit_chars` (500 to 1000000) stops a reply whose reasoning exceeds it: status `error`, error kind and finish reason `reasoning_limit`, reasoning kept up to the limit. Both are per request and default off; the browser keeps them per viewer in `localStorage`.
- **Attachments:** `attachments` (<= 5 files, <= 256 KiB each, <= 512 KiB total, valid UTF-8, no NUL) are appended to the new user message as `[Attached file: name]` plus a code fence longer than any backtick run in the file, so the content cannot close it. Names are reduced to a printable base name (no path, backticks, control or format characters, 100 characters). Provenance (`params_json`) stores names only. Regenerations take none.
- **Frontend:** `js/tools.js` adds code Copy/Save, rendered/source toggle, attachment reading (also by drop), prefs and scroll follow; `chat.js` consumes `snapshot`/`delta`/`end`, re-attaches on a short stream and on opening a thread whose reply is running, shows an elapsed timer and the end note, and no longer touches shared header controls after the thread was left.
- **Not implemented:** math rendering (needs a vendored library with license and hash inventory), generated-file Save beyond fenced code, persisted per-event replay across restart (the stored partial message is shown instead), provider-aware reasoning parsing beyond the existing `reasoning_content` field.

## 13. Lore, summaries and retrieval (parity milestone 5)

- **Packages:** `internal/memory` is pure (no database, no network): `ParseStorybook`, `Retrieve`, vector helpers and `Settings`. `internal/generate/memory.go` coordinates it with the store and providers. `internal/prompt.Assemble` takes the results (summary text, a `Retrieve` callback) and stays deterministic. Schema 4 (`migrations/0004_memory.sql`) adds `memory_settings` (one revisioned JSON row), `conversation_summaries`, `embedding_cache` and `retrieval_records`.
- **Authored lore vs. summary:** the card's `lore` field stays authored, always-on text in the system message. The running summary is separate and is stored per conversation, never in the card. A card also has an optional `storybook` (characters only, up to 256 KiB, parsed at save so a malformed or over-limit book is rejected): `# entity` sections or flat `entity_tags:` lines with `tags` (weights `term [0.8]`), `relation`, `use`, `role_in_arcs`, `offscreen_default`, dotted subtrees, and `---`-separated standalone prose. Unknown lines stay prose; nothing is executed.
- **Summary storage:** `conversation_summaries` is append-only (auto, manual, failed). A revision applies to a branch only when its anchor message and its `through_message_id` (the last archived message) are on the visible branch, so an edited or forked branch never inherits a summary of text it does not contain. Messages are never modified or removed by summarising; the prompt builder simply starts after the boundary. A failed pass is recorded (source `failed`, note) but never changes the boundary. A manual edit appends a revision with the same boundary; clearing appends an empty revision with no boundary. Writers pass the latest revision they saw (`expected_seq`); a mismatch is `409`, and an automatic pass that raced a hand edit has its result discarded.
- **When it runs:** with `summarize_enabled` (default off), before the turn is stored, `planArchive` compares fixed prompt cost + summary + live history with 72% of the context window (90% input budget minus a 20% reply reserve). Over that, it archives the oldest messages down to a 65% landing, always keeping the newest 22% of the budget verbatim and cutting on a user-turn boundary. The compression model is the configured (endpoint, model) pair, else the thread's model. The transcript is bounded by that model's window (messages clipped to 4000 bytes, a partial feed is allowed, a too-small window fails with "no room"); reasoning is ignored, labels and `<think>` blocks are stripped, output over 1.5x `summary_max_chars` is a failure and 1.0-1.5x is cut at a sentence end. After a failure the next automatic attempt is paused for 5 minutes; the prompt then falls back to the existing whole-turn trim (`omitted_messages`), reported in the record. `POST .../summary/compress` is the explicit user action (folds everything before the latest user turn). The pre-turn phase is tied to the request: if the browser aborts before the turn exists, preparation is cancelled and nothing is stored (`Jobs.StartContext`); once the turn exists the job is detached as before.
- **Prompt order:** one system message (system prompt, character fields, authored lore, `{{carousel}}`, then `[Summary of earlier conversation]`), the live history, one system message `[Relevant lore]` (retrieved entries, ranked), the persona tail, the user message. The summary is dropped with a note if it cannot fit beside the system text and the turn. Retrieval gets at most a quarter of the context and never what the fixed parts use; unused room returns to history. The persona cadence counts all user turns on the branch, archived or not. The budget unit is `memory.Cost` (UTF-8 bytes + 16), the existing conservative estimate, so "1 unit ~ 1 byte", not tokens.
- **Retrieval:** Retriever B (tags) fires an entity when summed matched weight reaches `fire_threshold` (Unicode word-boundary matching, no regular expression built from card text). Identity vs. thematic trigger, one relational hop (never into offscreen entities), warmth for `warmth_turns` extra turns (in memory, per conversation), offscreen suppression, ranking by score x arc-role, duplicate-text and budget dedupe. Retriever A (semantic) embeds the recent window and the storybook documents (entity prose and standalone chunks) and takes up to `top_k_a` above `semantic_floor`. Both feed one ranker; an entity costs budget once. With embeddings unavailable the turn is tag-only and the record says why.
- **Embeddings:** the chosen model must be in the cached inventory with type `embeddings`. If its cached state is `not-loaded`, no request is sent (LM Studio would load it just-in-time). One request per turn carries the query plus up to 31 uncached documents; vectors are cached in `embedding_cache` by SHA-256 of (prefix + text) and exact (endpoint, model), so a new model or changed text never reuses a stale vector. The cache is bounded (20,000 rows, oldest trimmed) and clearable. Timeout 20 s, the endpoint slot is honored, any failure degrades to tag-only.
- **Telemetry:** one `retrieval_records` row per generation that used a storybook, a semantic request or summary activity (last 50 per conversation, cascade-deleted): candidates with trigger, score and skip reason, budget numbers, semantic status, context fit and notes, shadow thresholds. Identifiers, scores and costs only; no message or lore text. The prompt snapshot in `generations.params_json` also records `summary_seq`, `archived_messages`, `retrieved` ids and cost.
- **API:** `GET/PUT /api/memory/settings` (revisioned), `POST /api/memory/storybook/preview` (parse only), `GET/DELETE /api/memory/embeddings`, `GET /api/conversations/{id}/memory` (effective summary, history, records), `PUT /api/conversations/{id}/summary`, `POST /api/conversations/{id}/summary/compress`.
- **Frontend:** `js/memory.js` (MEMORY dialog with SUMMARY, INSPECTOR and SETTINGS tabs, plus a header chip) and a storybook field with a "check" action in the character editor (`js/cast.js`).
- **Not implemented / limits:** summaries are not exported in snapshots (they reference message ids), per-card compression models, token-exact budgets, GobboNet snapshot import. Semantic retrieval is verified against a scripted OpenAI-shaped server only (`PROVIDER-SPEC.md` section 5.3); no live embedding request has been made.


## 14. Web search and MODS (parity milestone 6)

- **Packages:** `internal/search` (settings, query building, response sanitising, the one `search.Run` call), `internal/mods` (macro and appearance validation, built-in presets), `internal/egress/search.go` (`SearchClient`), `internal/store/mods.go`, `internal/generate/websearch.go`, `internal/api/mods.go`. Schema 5 (`migrations/0005_mods_search.sql`) adds `search_settings` (one revisioned JSON row plus the DPAPI-protected key), `search_records`, `macros` and `appearance_settings`.
- **Search service:** one fixed destination, `https://ollama.com/api/web_search` (the service the pinned GobboNet proxy forwards to), `POST {"query","max_results"}` with `Authorization: Bearer <key>`, answered `{"results":[{"title","url","content"}]}`. The host, scheme and path are constants in `internal/egress`; no setting, request body or model output can change them, and the path is deliberately **not** in the provider path allowlist. The dial guard requires a public address (class `external`), refuses redirects and metadata addresses, and bounds the response at 1 MiB and the call at 20 s. This is the contract from the pin and the service's published API; no live request has been made (it needs an owner key).
- **Opt-in, three gates:** the owner enables search and stores a key (MODS, SEARCH tab), and each message needs the composer box ticked (`web_search: true`). Off by default. A request that asks for search while it is off, keyless or unavailable is refused with `409` before anything is stored or sent. `POST /api/search/test` is the one explicit owner action that contacts the service without a message; it sends the fixed phrase "labbonet connection test", never conversation text.
- **Query and results:** the query is the typed message only: text before any attachment block, `{{macro}}` tags removed (neither expanded nor sent), whitespace collapsed, at most 400 characters. Results are bounded and sanitised (at most 10; title 200 characters; URL 500 bytes and plain `http(s)` with a host and no credentials; content 1200 characters; control characters removed); anything else is dropped. A failed search (network, HTTP error, bad JSON) is recorded and the reply continues without results; abandoning the request cancels the search and stores nothing.
- **Prompt placement:** after the history and lore, before the persona tail and the user message, one system block `[Web search results for: "..."]` that says the text is from external websites, unverified, and not to be obeyed, lists numbered results with their sources, and ends with `[End of web search results]` (that marker inside result text is neutralised). It takes at most a quarter of the context minus what the fixed parts use; results are dropped from the end until it fits, and if none fit the search is recorded as omitted. Results apply to that turn only (they are not saved into history), so later turns neither resend them nor keep paying for them; this is a deliberate difference from GobboNet, which saves them on the user message.
- **Records and citations:** `search_records` holds, per generation, the query, the sanitised results (snippet cut to 400 characters), how many reached the prompt, and any error; the newest 50 per conversation, cascade-deleted. `generations.params_json` keeps the exact prompt and `web` (title, URL) provenance. The key never appears in a record, params, prompt, error text, log or backup: `Store.Backup` clears `search_settings.credential_blob` like endpoint credentials.
- **Macros:** global `{{name}}` text. Names start with a letter and use letters, digits and `_` (up to 40), are unique ignoring case, and are not one of `char user date time carousel current_dat auto_continue`; text is 1 byte to 8 KiB; at most 100 macros and 128 KiB in total. Saving validates the whole set: each macro must expand within the prompt's limits (16 levels, 256 KiB, cycle detection), so a loop or a growth bomb is refused. Lookup is case-insensitive, as upstream's is. Precedence: built-ins, then persona, then the character's own macros, then global macros. They expand in the system text and character fields as before and, new here, in the current user message and earlier **user** messages for the model only; stored messages keep the tag, assistant text is never expanded, and `{{date}}`, `{{time}}` and `{{carousel}}` stay literal in history. An expansion that fails (cycle, size) refuses the send before anything is stored. Edits echo the revision (`409` if stale); a trigger cannot be renamed (delete and add).
- **Appearance presets:** a preset is a name plus values for an allowlist of CSS custom properties: ten colours (`--black --surface --elevated --border --cyan-bright --cyan-mid --cyan-dim --magenta --magenta-dim --white`, exactly `#rrggbb`) plus `--font-scale` (0.8 to 1.4) and `--avatar-scale` (0.5 to 2). Five built-ins (default, amber, ice, green, large text) are code; up to 12 custom presets are stored with server-assigned ids. The browser re-checks each value's shape and applies it with `style.setProperty`/`removeProperty` on `:root` (CSSOM, so the strict CSP stays), at page load and when saved; an unsaved preview is reverted on close. `--font-scale` scales the message text and the composer; it is not a global zoom.
- **Reset:** a full DATA reset also clears macros and the appearance selection; it leaves the search key and settings (a credential, like endpoints). Macros and presets are not part of snapshots or exports yet.
- **Frontend:** `js/mods.js` (MODS dialog: MACROS, APPEARANCE, SEARCH, EXTENSIONS tabs; `applyAppearance`, `bootAppearance`), `js/search.js` (composer toggle that is only enabled when search is ready; citations under replies, set with `textContent` and a link check), `chat.js` wiring, `app.css`.
- **Intentional parity exceptions:** GobboNet's extension loader (script URL or inline code, stylesheet URL or inline CSS, a status list of loaded extensions) is not reproduced; presets cover colours and sizes only. The search relay is a fixed-service client instead of a local reverse proxy that holds the key for the browser. Results are not persisted into the message history.
- **Not implemented / limits:** macros and presets in snapshots, import of GobboNet macro lists, other search providers, page fetching, a per-thread search default, a live search call.

## 15. Scheduler, dashboard and settings profiles (parity milestone 7)

- **Storage and ownership:** schema 6 (`0006_schedule_profiles.sql`) adds `schedules`, `schedule_runs`, `profiles`, `profile_state`. `internal/schedule` owns pure validation/time rules, `internal/profile` the settings bundle, store counterparts persistence, `internal/generate/scheduler.go` orchestration. Conversation deletion sets schedule targets to NULL so the next occurrence reports why it could not run. Full reset deletes schedules/profiles; other reset scopes preserve them. Upgrade retains the credential-free pre-migration backup contract.
- **Timing:** server timer checks every 15 seconds with a five-minute grace period. Once/daily schedules store explicit IANA zones and a UTC next occurrence. Daily wall time survives DST: a repeated minute uses its first occurrence; a missing minute advances to the first valid minute on that date. A completely skipped date has no run. Once schedules disable after the occurrence is claimed. A late occurrence outside grace creates one missed ledger entry and advances to the next future occurrence, never a burst of catch-up requests.
- **Claims and runs:** compare-and-swap on due time and revision plus a unique scheduled-occurrence index prevents duplicate claims. Run-now rejects an already-running schedule and does not change its next occurrence; it also works when disabled. Edited/deleted schedules stop subsequent follow-ups. Runs use the existing detached generation service, endpoint queue and conversation conflict checks. Busy/deleted threads or unavailable endpoint/model selections are reported; cached not-loaded models are refused without a confirmation override. Stop/shutdown cancel pre-turn preparation; cancellation before generation registration is carried forward once the job receives its id.
- **Bounds:** 50 schedules, 8 KiB prompt, 0-5 fixed `Continue.` follow-ups (at most six turns), 60000-character reasoning watchdog per reply, latest 50 ledger entries per schedule. Search applies only to the initial prompt and requires enabled search plus the stored key. Restart marks unfinished ledger entries interrupted, never resubmits them. Shutdown stops the timer before canceling jobs and waits for ledger completion.
- **Profiles:** at most 20 uniquely named settings bundles capture the appearance preset id, optional memory settings and browser reply cap/reasoning limit/follow. Capture reads server settings; browser supplies only its prefs. Apply reports each applied component and any unavailable preset/settings in notes. Profile updates/deletes use revisions. The last-applied id is a selection record, not proof the live settings still equal that bundle. Profiles share conversations/cards/endpoints in one database; isolated data profiles and profile snapshot export are not implemented.
- **Frontend:** `schedule.js` offers editing, enable/duplicate, run-now, ledger, Stop and both local/zone next times. It states that server uptime is required while browser uptime is not. `home.js` shows character shortcuts, recent threads, schedule status, connections and profile capture/apply/update/delete. Profile prefs map between snake_case API and camelCase browser storage. Document events connect dashboard navigation to the sidebar; home polling/listeners are disposed on thread navigation. No untrusted HTML is introduced.

| API | Behavior |
|---|---|
| `GET/POST /api/schedules`, `PUT/DELETE /api/schedules/{id}` | List/status/limits, create, revisioned edit/delete |
| `POST /api/schedules/{id}/duplicate`, `POST .../{id}/run` | Disabled copy, immediate detached run |
| `GET /api/schedules/{id}/runs`, `POST /api/schedule-runs/{id}/cancel` | Bounded ledger and cancellation |
| `GET/POST /api/profiles`, `PUT/DELETE /api/profiles/{id}`, `POST .../{id}/apply` | List/last applied, capture, revisioned update/delete, apply with notes |

All routes retain the existing session/Origin/header checks. Credentials never enter profile bundles or the schedule ledger. GobboNet behavior references and independent implementation boundaries are in `REUSE-LOG.md`.
