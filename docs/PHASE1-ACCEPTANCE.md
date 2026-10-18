# Phase 1 Acceptance

**Version:** 0.1 (2026-10-18)
**Status:** Draft for owner approval (DR-015 "feature acceptance tests documented" and "non-goals explicitly listed").
**Milestone:** a secure, loopback-only, multi-endpoint chat client with streaming, cancellation, durable conversations and diagnostics, proven on Strata and LM Studio (DR-015 Option B).

**Exit criterion (proposal section 20):** create a conversation, choose the Strata model, receive a streamed reply, switch the same conversation to an LM Studio model and continue, with no restart and no lost history.

---

## 1. Scope

In scope: browser chat UI, endpoint registration and testing, model discovery, per-conversation model selection, streaming and cancellation, SQLite persistence with migrations and backup, connection diagnostics, DPAPI credential storage, the controls in `SECURITY-MODEL.md`.

## 2. Non-goals for Phase 1

- WebLLM (Phase 2).
- Characters, personas, lorebooks, card import (Phase 3).
- Embeddings, RAG, summarization, memory (Phase 4).
- Role assignment, parallel or comparison runs, routing, fallback (Phase 5; DR-014 deferred).
- Message editing, branching, response variants (the schema keeps `parent_id` so they can come later).
- Serving Labbonet to other devices, TLS, multi-user (DR-010 B).
- Portable JSON export and import (DR-016; Phase 2 or 3). Phase 1 has database backup only.
- Ollama, llama.cpp and gateway adapters beyond what the generic OpenAI adapter already handles.
- Loading, unloading or configuring models on any provider.
- Web search, macros, scheduling, extensions.

## 3. Prerequisites the owner provides

| ID | What | Needed for |
|---|---|---|
| P1 | Strata running at `127.0.0.1:8080` with a model loaded | Most Strata tests |
| P2 | At least one LM Studio chat model loaded on `10.138.11.11:1234` (the probe found none loaded) | LM Studio tests that must not trigger JIT |
| P3 | A short window where the owner sets a Strata `api_key`, then removes it | A-ST6 |
| P4 | A short window where the owner enables LM Studio "Require Authentication" and creates a token | A-LM6 |
| P5 | A cold Strata start (the owner restarts Strata; Labbonet never unloads) | A-ST5 |

## 4. Functional acceptance tests

Labels: **auto** = automated test against a fake OpenAI-compatible server in `tests/`; **live** = run against the real host; **manual** = UI walkthrough.

### 4.1 Backend and storage

| ID | Test | Type | Pass condition | Covers |
|---|---|---|---|---|
| A-B1 | Start as a standard user | live | Listens on `127.0.0.1:4815`, prints launch URL, no elevation prompt | DR-002, DR-010 |
| A-B2 | Port 4815 already in use | auto | Exits with a clear error; the other process is untouched | DR-002 |
| A-B3 | Second instance on the same data directory | auto | Refused by lock file | DR-006 |
| A-B4 | Ctrl+C during a stream | live | Partial reply saved as `cancelled`; database closes cleanly | DR-002 |
| A-B5 | Fresh start creates the schema; restart keeps data | auto | `user_version` equals the latest migration; rows survive | DR-006 |
| A-B6 | Upgrade from migration N-1 | auto | Migrates in one transaction; data intact | DR-006 |
| A-B7 | Database newer than the binary | auto | Refused, file unmodified | DR-006 |
| A-B8 | Backup then restore (copy backup over the database) | auto | Conversations identical; credential table empty in the backup | DR-006, DR-016 |

### 4.2 Endpoints and models

| ID | Test | Type | Pass condition | Covers |
|---|---|---|---|---|
| A-E1 | Register Strata and LM Studio; restart | live | Both persist with name, URL, class (`local`, `lan`) | Proposal 6.1 |
| A-E2 | Connection test detects provider type | live | Strata detected via `/health`; LM Studio via `/api/v0/models` | DR-005 |
| A-E3 | Model discovery | live | Strata lists its loaded model; LM Studio lists all models with type, state and context | Proposal 8.2 |
| A-E4 | Selector grouping | manual | Models grouped by endpoint, embeddings hidden, state and privacy class visible | Proposal 18 |
| A-E5 | Offline endpoint | auto | Test reports `unreachable`; the rest of the app works | Proposal 12.4 |
| A-E6 | Two endpoints advertise the same model ID | auto | Kept as separate (endpoint, model) identities | Proposal 8.1 |
| A-E7 | Set, replace and clear a credential | auto | Stored DPAPI-protected; API reports only `credential_set` | DR-012 |

### 4.3 Chat, streaming and cancellation

| ID | Test | Type | Pass condition | Covers |
|---|---|---|---|---|
| A-C1 | Streamed reply from Strata | live | Tokens appear progressively; reasoning separate; `usage` and timings recorded | DR-005 |
| A-C2 | Streamed reply from LM Studio (loaded model) | live | Same as A-C1; reasoning field and `stats` presence recorded in `PROVIDER-SPEC.md` | DR-015 |
| A-C3 | **Exit scenario:** Strata reply, switch the conversation to LM Studio, continue | live | All earlier messages kept; each reply's provenance names its endpoint and reported model | DR-015 |
| A-C4 | Stop button mid-stream | auto + live | Upstream connection closed within 1 s; partial text kept as `cancelled` | DR-005 |
| A-C5 | Browser tab closed mid-stream | auto | Message marked `interrupted`; upstream cancelled | Proposal 21 |
| A-C6 | Upstream drops the connection mid-stream | auto | `error` status, partial text kept | Proposal 21 |
| A-C7 | Context overflow | auto | `context_overflow` error shown; history intact | Proposal 21 |
| A-C8 | Unknown model ID | auto | Generation records the reported model, not the requested one | `Strata_Protocol.md` 5.2 |
| A-C9 | Two conversations send to Strata at once | live | Second waits for the endpoint slot (limit 1); both complete | Proposal 9.3 |
| A-C10 | Send to an LM Studio model in state `not-loaded` | manual | Confirmation shown naming the model and the auto-evict risk; nothing sent until confirmed | `PROVIDER-SPEC.md` 6 |
| A-C11 | Provenance record | auto | Endpoint, requested and reported model, params, start, first token, finish, usage, finish reason stored | Proposal 10 |

### 4.4 Diagnostics

| ID | Test | Type | Pass condition | Covers |
|---|---|---|---|---|
| A-D1 | Diagnostics panel after a few requests | manual | Shows endpoint, model, status, time to first token, tokens per second, duration, errors | Proposal 18 |
| A-D2 | Diagnostics and logs contain no message text or secrets | auto | Byte search finds neither | DR-012 |

### 4.5 Provider-specific open items

| ID | Test | Type | Needs | Pass condition |
|---|---|---|---|---|
| A-ST5 | Strata cold load | live | P5 | First-byte timeout (180 s) covers the load; health shows `loaded: false` then `true` |
| A-ST6 | Strata authenticated mode | live | P3 | 401/403 without the key, success with it, key never sent to LM Studio |
| A-ST7 | Strata cancellation | live | P1 | `/status` shows `busy: false` within a few seconds of stop |
| A-LM6 | LM Studio authenticated mode | live | P4 | Same as A-ST6 |
| A-LM7 | LM Studio cancellation | live | P2 | Generation stops server-side (LM Studio UI or a following request starts at once) |

## 5. Security acceptance

All tests S1 to S17 in `SECURITY-MODEL.md` section 10 pass.

## 6. Sign-off

Live progress (2026-10-17 America/Los_Angeles): `evidence/step7-live.json` records A-C1/A-C2/A-C3, A-C9, A-ST7/A-LM7 and A-B4. The two replies in the exit conversation retain distinct endpoint/requested/reported model provenance. The shutdown check kept 188 content characters and 195 reasoning characters as `cancelled`, removed the lock, and returned SQLite `integrity_check: ok`. A-B1/S17 ran under a standard Windows account (`WindowsPrincipal.IsInRole(Administrator)` was false). S16 inspection found no forbidden application command calls; the binary substring `netsh` was inside dependency symbols `NetShareAdd`/`NetShareDel`, not a `netsh` invocation. The sole application process launcher opens the default browser with `cmd /c start`.

Owner release decision (2026-10-18): current development is **V0-2-0**. **P3-P5 and A-ST5, A-ST6, A-LM6 (provider auth/cold-start checks), together with final Phase 1 sign-off, are deferred until V2-0-0.** They are not passed or waived and do not block current GobboNet parity work. The owner also rejected the functional shell's visual resemblance to GobboNet; frontend visual acceptance remains open. The saved `evidence/step7-exit-ui.png` is evidence of the working conversation, not visual approval. Detailed security/acceptance document review remains open; final Phase 1 sign-off is scheduled for V2-0-0.

When resumed for V2-0-0, Phase 1 is complete when every test above passes or has an owner-approved waiver recorded here, and the DR-002, DR-005, DR-006, DR-012 and DR-015 acceptance criteria in `decision_register.md` are ticked with a pointer to the evidence.

| Date | Result | Waivers | Approved by |
|---|---|---|---|
| 2026-10-18 | Remaining provider auth/cold-start checks and final sign-off deferred until V2-0-0; current development V0-2-0 | No waiver or pass recorded; visual parity remains current scope | Project owner (chat instruction) |


## Parity milestone 1 verification progress (2026-10-18)

Implementation is wired; this is additional progress, not Phase 1 sign-off or completed application parity.

| Check | Result / evidence |
|---|---|
| Two characters and two personas survive store reopen | Automated pass: `internal/store/cast_test.go` |
| Revisions, wrong kinds, stale delete/selection, atomic order, frozen defaults and historical names | Store/API tests pass; browser two-tab stale-save conflict displayed instead of overwriting |
| Populated schema 1 upgrade, credentials preserved live/redacted from pre-upgrade backup; rollback on failure | Automated pass: `internal/store/cast_migration_test.go` |
| Fake provider gets resolved prompt/card params; queued card edit cannot change request or speaker | Automated pass: `internal/generate/cast_test.go` |
| Invalid macro/oversized prompt/images/params; no new turns on prompt rejection; immediate cancel | Cast/prompt/generate/API tests pass |
| Browser CRUD creation/copy/reorder/defaults, per-thread selection, explicit greeting, reload and historical speaker after switch | Exercised on isolated temp database at port 4817; `evidence/cast-library-wide.png`, `cast-library-panel.png`, `cast-chat-wide.png` |
| CSP-safe text/dialog colors, sanitizer | Computed colors match saved hex values, no inline style nodes/attributes; five S14 cases pass; no warnings/errors before intentional conflict |
| Public source attribution | `evidence/gobbonet-attribution-about.png`; `REUSE-LOG.md`, `GOBBONET-PROVENANCE.json`, root `NOTICE.md` |
| Unsaved-editor Escape protection | Browser emitted `Discard unsaved card changes?`; native-dialog dismiss/retain path still needs a reliable walkthrough (browser connection stalled during prompt handling) |
| Browser file upload/preview/removal | Pending: extension file chooser refused local file access because Chrome's “Allow access to file URLs” permission is off. No permission/settings change made. Full raster validation is tested separately. |
| Exact pinned visual comparison and owner visual acceptance | Open in V0-2-0; not inferred from implementation/tests |
| Remaining Phase 1 provider auth/cold-start checks and final Phase 1 sign-off | Owner-deferred until V2-0-0; not passed or waived |

No live inference requests or model-management operations were made in this continuation. The browser work used a separate temporary data directory; the owner's database was not migrated by this verification instance.
