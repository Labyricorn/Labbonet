# Labbonet Security Model

**Version:** 0.1 (2026-10-18)
**Status:** Draft threat model for Phase 1 (DR-012 "threat model completed" is met only after owner review). Scope: one user, one Windows machine, backend on loopback, providers on the local machine or a trusted LAN.

---

## 1. Assets

| Asset | Where it lives |
|---|---|
| Conversation content (prompts, replies, reasoning) | SQLite, browser memory, in transit to providers |
| Provider credentials (API keys, bearer tokens) | DPAPI-protected blobs in SQLite; backend memory while in use |
| Endpoint configuration and privacy classification | SQLite |
| Labbonet session token | Backend memory, browser cookie |
| Provider resources (loaded models, GPU memory, request slots) | The provider hosts, shared with other clients |
| Files outside the data directory | User's disk (must never be touched) |

## 2. Actors

| Actor | Trust | Main concern |
|---|---|---|
| The local user | Trusted | Mistakes: sending private data to the wrong endpoint, losing history |
| Any web page open in the user's browser | **Untrusted** | Calling the loopback backend (CSRF, DNS rebinding) to read conversations, spend provider resources or register an attacker endpoint |
| Other local processes and other OS users | Untrusted | Loopback is reachable by every local process |
| Model output, imported content (later: cards, lorebooks, web results) | Untrusted data | HTML/script injection, prompt injection redirecting requests |
| Hosts on the LAN | Partly trusted | Plain-HTTP traffic to the LM Studio host can be read or altered on the network |
| Provider servers | Trusted for inference only | Must not receive another endpoint's credentials or unapproved data |

## 3. Trust boundaries

1. Browser page to Labbonet backend (loopback HTTP).
2. Backend to each provider endpoint (loopback or LAN HTTP).
3. Backend to disk (data directory only).
4. Rendered content to the DOM (model output is data, never markup or code).

## 4. Controls: browser to backend

| Control | Rule | Stops |
|---|---|---|
| Loopback bind | Listen on `127.0.0.1:4815` only; Phase 1 rejects any other `--host` | LAN exposure (DR-010 A) |
| Host allowlist | Every request must have `Host` equal to `127.0.0.1:<port>` or `localhost:<port>`; otherwise 421 | DNS rebinding |
| Origin check | Every `/api` request: if `Origin` is present it must equal the backend's own origin; `Sec-Fetch-Site`, when present, must be `same-origin` | Cross-site requests |
| Custom header | Every `/api` request must carry `X-Labbonet: 1`. Cross-origin pages cannot add it without a CORS preflight, and the backend never answers preflights with CORS headers | CSRF via simple requests |
| No CORS | The backend never emits `Access-Control-Allow-*` headers | Cross-origin reads |
| Session token | At start the backend generates a 256-bit random token and prints a launch URL with a one-time code. Visiting it sets an `HttpOnly; SameSite=Strict; Path=/` cookie and redirects to `/`. The code is single-use and expires after 2 minutes. All `/api` requests need the cookie | Other local processes and pages without the token |
| Security headers | `Content-Security-Policy` (below), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy: same-origin` | Injection, leaks, framing |

Phase 1 CSP:

```text
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:;
connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'
```

Phase 2 (WebLLM) will need `'wasm-unsafe-eval'` in `script-src`, `worker-src 'self'`, and `connect-src` for the approved model-artifact host. That change must be recorded here when it is made.

## 5. Controls: backend to providers (DR-012)

- **Explicit registration only.** The backend contacts a host only if it is a registered, enabled endpoint. Registering or editing an endpoint is a privileged API action (session required).
- **Fixed paths.** Requests are `base_url` plus one of the paths listed in `PROVIDER-SPEC.md`. User input, model output and imported content never form any part of a URL. Model IDs go in JSON bodies only.
- **Schemes:** `http` and `https` only. No credentials in URLs (`user:pass@` is rejected).
- **No redirects.** The egress client refuses every redirect.
- **Dial guard.** The address is resolved at connect time and must match the endpoint's privacy class: `local` means loopback only, `lan` means RFC 1918, link-local or ULA only, and `external` means a public address and must be acknowledged at registration. If DNS later resolves into a different class, the request is refused. Cloud metadata addresses (`169.254.169.254`, `fd00:ec2::254`) are always refused.
- **Limits.** Connect timeout, first-byte timeout (long, for cold loads), idle-chunk timeout, and maximum response sizes for non-streaming calls (for example 8 MiB for model lists).
- **Credential scoping.** A credential is stored per endpoint and attached only to requests to that endpoint's ID. It is never copied to another endpoint, never sent to the browser (the API reports only whether it is set), and never written to logs, diagnostics, exports or backups.
- **Privacy visibility.** Each endpoint shows its class (local machine, trusted LAN, external). The conversation view shows which endpoint will receive the next message. There is no automatic fallback (DR-014 deferred), so data never moves to a different endpoint without a user selection.
- **Control-plane calls are forbidden.** Labbonet never calls model load, unload, VRAM, settings or MCP endpoints (`Strata_Protocol.md` section 4).
- **Implicit loading needs user confirmation.** LM Studio loads a model on first request by default (JIT) and may auto-evict another client's model (`PROVIDER-SPEC.md` section 6). Sending to a model whose reported state is `not-loaded` requires an explicit confirmation in the UI. That confirmation counts as the explicit user command required by proposal section 6.3.

### 5.1 Plain-HTTP LAN endpoint

The LM Studio host `10.138.11.11:1234` is plain HTTP. Conversation content, and any token if authentication is enabled, crosses the LAN in clear text. It is classified `lan` (trusted LAN), and the UI warns when a credential is set on a plain-HTTP endpoint. Use it only on a trusted network, or through a tunnel the user sets up.

### 5.2 Observed third-party exposure (outside Labbonet's control)

- LM Studio on `10.138.11.11:1234` returned `Access-Control-Allow-Origin: *` and needed no authentication (observed 2026-10-18). Any web page in any browser on the LAN can therefore call it, and its JIT loading lets such a page load models. Labbonet does not need CORS on LM Studio, since all traffic goes through the backend. **Recommendation to the owner:** turn off "Enable CORS" and turn on "Require Authentication" in LM Studio's server settings.
- Strata on `127.0.0.1:8080` has no API key; it blocks foreign `Origin` headers but accepts any local process.

## 6. Controls: content and rendering

- Model output, reasoning text and (later) imported content are rendered as Markdown with raw HTML disabled, then sanitized. Links get `rel="noopener noreferrer"` and open in a new tab only on user click.
- No `innerHTML` with unsanitized strings, no `eval`, no inline scripts or styles (CSP enforces this).
- Remote images in model output are not fetched (CSP `img-src` blocks them).

## 7. Controls: disk

- All writes go inside the data directory. Paths are built from fixed names, never from user or model input.
- No uninstall routine, no recursive delete outside the data directory, and no deletion of anything Labbonet did not create.
- Logs contain request metadata only: no message content and no `Authorization` header values.

## 8. Supply chain

- Go dependencies pinned in `go.sum`. Frontend dependencies vendored with their SHA-256 recorded in `frontend/vendor/VENDOR.md`.
- No runtime script loading from CDNs or remote URLs.
- Dependency licenses recorded in `ARCHITECTURE.md` section 8.

## 9. Out of scope for Phase 1

- Serving Labbonet to other devices (DR-010 B: authenticated HTTPS LAN, later).
- Multiple OS users sharing one data directory.
- Protection against malware already running as the same OS user (DPAPI current-user scope does not protect against it).

## 10. Negative security tests (Phase 1)

Each one is an automated test in `tests/` unless marked manual.

| # | Test | Expected |
|---|---|---|
| S1 | Request with `Host: evil.example:4815` | 421, no handler runs |
| S2 | POST from `Origin: https://evil.example` with session cookie | 403 |
| S3 | `/api` request without `X-Labbonet` header | 403 |
| S4 | CORS preflight to `/api/*` | No `Access-Control-Allow-*` headers |
| S5 | `/api` request without the session cookie | 401 |
| S6 | Reuse of a spent or expired launch code | Rejected |
| S7 | Start with `--host 0.0.0.0` | Refuses to start |
| S8 | Endpoint whose server redirects to another host | Redirect refused, error shown |
| S9 | `local` endpoint whose name resolves to a non-loopback address | Refused by dial guard |
| S10 | Endpoint URL `http://169.254.169.254/` | Refused at registration |
| S11 | Credential set on endpoint A, request to endpoint B | B request has no `Authorization` header (checked with a capture server) |
| S12 | `GET /api/endpoints` after setting a credential | Response has `credential_set: true`, no secret |
| S13 | Logs, diagnostics, backup file after a run with a credential | Secret string not present (byte search) |
| S14 | Model reply containing `<script>`, `<img src=x onerror=...>`, `javascript:` link | Rendered inert; no script runs (browser test) |
| S15 | Chat to an LM Studio model in state `not-loaded` without confirmation | Request not sent |
| S16 | Search the binary and source for firewall, `netsh`, PowerShell and process-kill calls | None found (manual or scripted check) |
| S17 | Run as a standard user without elevation | All Phase 1 features work (manual) |


## 11. In-progress cast/prompt controls

The parity milestone 1 endpoints retain the existing session, Host, Origin, fetch-site and `X-Labbonet` checks. Card data is inert, strict JSON (4 MiB request cap), with bounded fields/macros and optimistic revisions. PNG/JPEG files are embedded, limited to 1 MiB and 4096 / 16 MP, checked by full raster decoding; remote image URLs and user-supplied SVG are refused. No provider management paths or executable card hooks are added.

Prompt expansion has depth/size/cycle bounds and uses no interpreter. Rejected prompts persist no turns. Whole-turn budgeting is a conservative client estimate, not a model tokenizer guarantee. Cards and resolved prompt provenance are read before endpoint queuing; history-count/cast-revision/streaming checks prevent overlapping same-thread turns. Historical speaker identity survives card changes/deletion. Prompt provenance does not duplicate image assets; portraits remain in message snapshots.

Model/greeting text is sanitized or rendered with text nodes. Hex colors enter fixed CSSOM rules only after validation; card text cannot supply selectors, stylesheet text or URLs. CSP and endpoint allowlist are unchanged. New tests cover image corruption, parameter/template rejection, conflicts, atomic ordering, upgrades/redacted backups, prompt/speaker snapshots and immediate cancellation. Owner security-document review and original deferred provider windows remain open.

## 12. Import, export and reset (parity milestone 2)

Uploads are untrusted. Limits, magic-byte dispatch, PNG chunk/CRC checks, ZIP entry/name/ratio/executable checks, full raster decoding with metadata stripping and strict versioned snapshot validation run before any database write; the import itself is transactional and a preview rolls back. Remote URLs in cards are never fetched, script-like card extensions are stored as inert text, and unsupported content is reported. Exports exclude endpoints, credentials, models, provider settings and generation parameters (asserted by test). Reset needs an exact typed phrase server-side, writes a credential-free backup first, and cannot name endpoint or credential scopes. Covered by `internal/interchange`, `internal/store/snapshot_test.go` and `internal/api/data_test.go`: malformed JSON/PNG/ZIP, traversal, executable entries, decompression bombs and header lies, duplicate IDs/keys, wrong-kind references, failure-injected rollback, unchanged database after rejection, no credential leakage.

## 13. Message tree, organization and search (parity milestone 3)

New endpoints sit behind the existing session, Host, Origin, fetch-site and `X-Labbonet` checks and a 4 MiB body cap; none reaches a provider except generate, whose allowlist and slot rules are unchanged. Tree edits cannot overwrite another tab's work: edit, delete and switch require the conversation revision (missing is `400`, stale is `409` with no write), are refused while a reply streams, and are scoped by conversation id so a message of another conversation is `404`. Edited text is bounded (1 MiB, valid UTF-8, non-blank) and rendered through the existing sanitizer; originals are never rewritten. Deleting uses one recursive statement inside the transaction. Only the visible branch is sent to providers, so a hidden variant cannot leak into a prompt (asserted with a capturing fake provider). Tags, folder names and thread order are validated (length, control characters, uniqueness, complete-set order) and shown only as text nodes. Search escapes `%`, `_` and `\` so user input is a literal substring, is length-bounded and capped at 500 results. Snapshot version 2 is validated in full before any write (acyclic parents, unique keys, valid selections/origins, bounded tags) and a rejected import leaves the database unchanged. Covered by `internal/store/branch_test.go`, `branch_snapshot_test.go`, `internal/api/branches_test.go` and `internal/generate/branch_test.go`.

## 14. Detached generation, attachments and reply limits (parity milestone 4)

Detached replies add no outbound path: they use the same `Runner`, allowlisted egress and slot rules, and the new `GET /api/generations/{id}/events` and `/api/conversations/{id}/generation` sit behind the existing session, Host, Origin and `X-Labbonet` checks and are read-only. A detached job cannot be started or stopped except by an authenticated request; shutdown cancels every job and waits (bounded) before the database closes, and a killed process leaves only `interrupted` messages that are never resubmitted. Replay memory is bounded by the reply and a slow reader is dropped rather than buffered without limit. The generate body is capped (1 MiB + 2 x 512 KiB). Attachment content is validated server-side (count, sizes, UTF-8, no NUL), fenced so it cannot terminate its own block, and stored as ordinary user text rendered through the sanitizer; file names are reduced to a printable base name and never used as paths. Browser code-save names come from a fixed extension table, not model text, and downloads use a Blob. Reply cap and reasoning watchdog are range-checked (`400` outside the range) and can only shorten a reply. Covered by `internal/generate/limits_test.go`, `jobs_test.go`, `internal/store/recover_test.go` and `internal/api/generate_test.go`.

## 15. Lore, summaries and retrieval (parity milestone 5)

Storybooks, summaries and retrieved text are inert text. Storybook parsing is bounded (256 KiB, 500 entities, 64 tags each, 2000 prose chunks) and builds no regular expression from card text; a card with an over-limit storybook is rejected at save and import. Only characters can carry one; export and import round-trip it in the Labbonet extension block, and snapshots stay credential-free. Injected lore and summaries are model-visible prompt text only: they cannot add requests, tools, paths or settings, and the browser renders them with `textContent`. The only new outbound path is the exact `/v1/embeddings` (POST) on an already-registered endpoint through the same egress client (privacy-class dial guard, redirect refusal, size cap); the path allowlist stays exact (tested).

Controls against unrequested model loads: an embedding model whose cached state is `not-loaded` is never called; a not-loaded compression model is skipped (fallback to the thread's model if that is loaded, else a recorded failure); neither setting can be saved with a model absent from, or of the wrong type in, the endpoint's inventory; no request carries load, unload or TTL fields. Calls are bounded (embedding batch <= 64 inputs of 16 KiB, 8192 dimensions, finite values, 20 s; summary 90 s with input and output caps) and honor the endpoint's concurrency slot.

Summaries are user content: a hand edit is a new revision, an automatic pass that races it loses (revision check, `409`), a failed, empty or oversized result never advances the archive boundary, and no message is ever deleted or rewritten. Retrieval records keep identifiers, scores and costs only (asserted in tests), so the inspector cannot leak conversation or lore text. The settings, summary and preview APIs sit behind the existing session, Host, Origin and `X-Labbonet` controls with a 4 MiB body cap (storybook preview 256 KiB + 4 KiB). Retrieval degrades on any failure to fewer injections, never to a request error. Embeddings are derived data in the local database; they are part of the credential-free backup and a full reset clears them. Covered by `internal/memory/memory_test.go`, `internal/store/memory_test.go`, `internal/generate/memory_test.go`, `internal/providers/openai/embed_test.go`, `internal/prompt/assemble_test.go` and `internal/api/memory_test.go`.


## 16. Web search and MODS (parity milestone 6)

**New outbound destination.** Web search is the only outbound path that is not a registered provider endpoint, so it is held to a stricter rule than section 5. The destination is a compile-time constant (`https://ollama.com/api/web_search`, `internal/egress/search.go`). No setting, API field, model output, imported card or search result can change the host, scheme or path; the path is not in the provider allowlist, so a provider endpoint can never be asked for it, and `NewSearchClientFor` (for test fakes) is asserted by test to be unused outside `internal/egress`. The dial guard requires a public address (class `external`), refuses cloud metadata addresses and every redirect, and the response is capped at 1 MiB and the call at 20 s. A response cannot cause a further request: links are never fetched, only shown.

**Explicit opt-in, with the boundary visible.** Search is off by default. Nothing is sent unless the owner has enabled it, stored a key, and ticked the composer box for that message. The server enforces all of it: `web_search: true` while search is off, keyless or unavailable is a `409` before any turn is stored or request made. The text sent is the typed message only (no attachment text, no `{{macro}}` tags, at most 400 characters); the MODS SEARCH tab and the composer tooltip say that text leaves the computer and where it goes. The connection test sends a fixed phrase, never conversation text, and only when the owner presses TEST.

**Credential.** The key is accepted once by `PUT /api/search/credential`, protected with DPAPI like endpoint credentials, stored in `search_settings.credential_blob`, and never returned (only `credential_set`). It is attached to the Authorization header of the one search request and nowhere else; it is not in search records, generation parameters, prompts, error text (upstream error bodies are not repeated), the access log, diagnostics or backups (`Store.Backup` nulls the column; a backup taken before schema 5 is handled). A full DATA reset keeps it, like endpoint credentials; removing it is the REMOVE KEY action.

**Untrusted results.** Result text is attacker-controllable. It is bounded and cleaned before storage (counts, lengths, control characters, URLs limited to plain `http(s)` with a host and no embedded credentials), injected only as a labelled system block that tells the model not to follow it, with the block's end marker neutralised inside result text, and shown in the browser with `textContent`; links open with `rel="noopener noreferrer"` and no referrer, and only after the browser re-parses them as `http(s)`. Results cannot add requests, tools, settings or paths. They apply to one turn only and are not stored in message history.

**MODS cannot fetch or execute.** Macros are plain text expanded by the existing interpreter-free `Expand` (16 levels, 256 KiB, cycle detection); the whole set is validated at save, built-in names are reserved, and a failing expansion refuses the send. Appearance presets are an allowlist of ten `#rrggbb` colours and two bounded numbers, validated on the server and again in the browser, applied through the CSSOM so CSP (`script-src 'self'; style-src 'self'`) is unchanged and no URL, selector, property name or stylesheet text can be supplied. GobboNet's script and stylesheet loaders are not implemented. Imported cards can create no macros, presets or search settings (tested with a card carrying such fields and script-like extension data).

**Concurrency and size.** Macros, appearance and search settings carry revisions (`409` on a stale save). Macros are capped at 100 items, 8 KiB each and 128 KiB in total; custom presets at 12; the new endpoints sit behind the existing session, Host, Origin and `X-Labbonet` checks with the 4 MiB body cap (the key endpoint accepts at most 512 characters).

New automated tests: `internal/search/search_test.go` (query building, URL/length/cleaning rules, keyed fixed-path request, error text never contains the key, redirect and oversize refusal, external class refuses loopback), `internal/egress/search_test.go` (fixed destination, allowlist untouched, no other constructor use), `internal/mods/mods_test.go`, `internal/store/mods_test.go` (revisions, set validation, key kept out of backups, reset scope, schema 4 to 5 upgrade), `internal/prompt/mods_test.go`, `internal/generate/websearch_test.go` (results framed as untrusted, never sent when not asked or off, refusal stores nothing, failure continues, key not in record or params, attachments not sent, macros expand only for the model, abandoned search stores nothing) and `internal/api/mods_test.go` (key never echoed, refusals, import inertness in `data_test.go`).

| # | Test | Expected |
|---|---|---|
| S18 | Send with `web_search: true` while search is off, keyless or unavailable | `409`, no turn stored, no outbound request |
| S19 | Send without `web_search` while search is enabled | No outbound request, no search text in the prompt |
| S20 | Search key set, then settings, records, error text, params, diagnostics and backup inspected | Key absent everywhere; backup column empty |
| S21 | Search response with `javascript:`/`data:` URLs, oversize body, redirect to a metadata address, injected end marker | Bad links dropped, body and redirect refused, marker neutralised |
| S22 | Preset or macro carrying `url(...)`, a non-allowlisted property, a cycle or a growth bomb; card import with macro/appearance look-alike fields | Rejected (`400`); import changes nothing |

## 17. Unattended schedules and shared settings profiles

- Schedules are local owner-authored prompts targeting an existing thread, not executable tasks. Timezones are explicit, schedule count/prompt/follow-ups are bounded, and follow-ups use the fixed text `Continue.`. A run uses the normal prompt validation, endpoint allowlist, queue and conflict protection. No model-management endpoint or unattended confirmation override is added; cached `not-loaded` models are skipped.
- Each scheduled reply has a 60000-character reasoning watchdog and normal provider timeout/output limits. Stop cancels the current job and remaining chain even if it arrives before generation registration. Edits/deletion stop follow-ups. Server shutdown stops ticking before canceling jobs; restart marks unfinished runs interrupted without automatic retry.
- Timed claims compare due time and revision transactionally and have a unique occurrence index. Multiple tabs/ticks cannot duplicate one occurrence. More than five minutes late records a missed occurrence without replaying it. The ledger retains only 50 entries per schedule and reasons are bounded to 300 runes.
- Scheduled search preserves the global enable/key gates and explicit per-schedule opt-in. Only the initial scheduled prompt requests search. No key is stored in schedules, profiles or run reasons; search errors are sanitized by the existing search layer.
- Profiles contain validated settings, never credentials or database paths. They do not provide data isolation. Capture/apply retains appearance and memory validation; unavailable components produce notes. Existing declarative-only MODS and fixed-destination search boundaries remain intact.
- Tests cover DST gap/repeat exact instants, UTC boundaries, concurrent claims, stale snapshots, missed/restart recovery, deleted/busy threads, not-loaded model refusal, bounded chain, single Stop before registration, revisions, reset scopes, migration preservation and credential-free backup. Profile API tests cover capture/apply with a missing preset and stale updates. Browser checks and remaining gaps are recorded in the handover; no live-provider claim is implied.