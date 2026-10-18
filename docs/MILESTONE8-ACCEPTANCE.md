# Milestone 8 — parity audit and visual review

2026-10-18, America/Los_Angeles. Development build **V0-2-0**.
Reference: GobboNet 1.7.6 at `314bdc9cae204ae38fdc1002ac6a9fb1437c109f`.

The milestone 8 implementation/audit pass is complete. **Full application parity
and independently measured pixel equality are not claimed.** Owner visual approval
passed on 2026-10-18 for the implementation at `17ce260`;
the limitations below are recorded, not silently waived. This record supplements
the earlier milestone evidence and does not grant Phase 1 sign-off.

## Inventory disposition

“Verified” means the stated checks, not exhaustive browser coverage or real-provider
certification. Previous browser/restart evidence is described in `devlog.md` and
the earlier `docs/evidence/` captures. Every area has automated coverage, browser
evidence, or an explicit limitation below.

| Plan inventory row | Implementation and verification | Disposition |
|---|---|---|
| Home/dashboard | Pinned landing CSS, section headers, schedule/character tiles, original safe starter cards; install and reload exercised; wide/420 px captures | Implemented; owner visual approval passed 2026-10-18 |
| Characters | Revisioned CRUD/copy/default/order, prompt fields, raster assets; store/API/prompt tests; real chooser upload/preview/remove/reupload/save now exercised | Implemented; context/sampler limits below |
| Personas | CRUD/default/order/cadence and immutable mixed-cast identity; cast/store/runner tests and prior browser checks | Implemented |
| Prompt templates | Macro precedence, dates, carousel, persona cadence, bounded prompt snapshots; prompt and runner tests | Verified with fake providers |
| Card interchange | Native/Tavern V1/V2/V3/PNG/CHARX tests; actual PNG export/download and GobboNet JSON chooser preview/commit now exercised | Limited compatibility; see below |
| Thread organization | Pins/tags/folders/content search/thread order; folder ordering added with complete-list conflict checks, API/store tests, browser reorder/reload and two-tab 409 | Implemented; touch drag not certified |
| Message history | Immutable variants/branch continuation/fork/provenance tests and prior browser walkthrough; message Delete Cancel/Confirm now exercised | Implemented; snapshot provenance limitation below |
| Generation lifecycle | Detached jobs/snapshots/Stop/queue/restart tests; previous browser reconnect, caps/watchdog and actual process-restart evidence | Implemented; remaining provider checks unchanged |
| Rendering and files | Native MathML, bounded explicit-language highlighting, exact code Copy/Save, `file:name.ext` Save, source toggle, real attachment chooser; 13 Chrome renderer checks | Verified; supported syntax/attachment limits below |
| Scroll/editor behavior | Follow release/jump/source prefs and prior browser checks; dirty-card Keep/Back/Escape/Discard now verified using in-app dialogs | Implemented; long-stream/touch behavior not exhaustively certified |
| Lore/summarization | Branch-aware summaries/history/editor/conflicts; scripted provider and store tests, prior browser inspector/editor checks, current narrow Memory access | Implemented; real summarizer verification owner-deferred |
| Hybrid RAG | Parser/tag/warmth/ranking/dedupe/budget/cache tests and scripted embeddings; no unloaded embedding calls | Implemented; real embedding verification owner-deferred |
| Web search | Fixed service/enable/key/request gates, redaction and hostile result tests; previous fake-service browser evidence | Implemented; real search verification owner-deferred |
| MODS | Macro bounds/cycles, safe appearance allowlist/CSSOM, inert import tests and prior browser checks | Implemented adaptation; JS/arbitrary CSS excluded |
| SCHED | Timezone/DST/claims/Stop/missed/restart tests and prior fake-provider browser checks; unavailable-model run ledger re-exercised without inference | Implemented; service coverage is not a browser-closed live run certification |
| DATA | Atomic preview/commit/reset/rollback/credential-free backup tests; actual file chooser, GobboNet compatibility report and bulk-thread download verified | Implemented with narrowed interchange boundary |
| Settings/presets | Supported sampler presets/reasons, declarative theme presets and shared-database profiles; capture/apply tests and prior browser checks; starter install and profile delete now exercised | Implemented adaptation; export/isolation limits below |
| Persistence/sync | SQLite authority, revisions, migration/backup tests, reload/restart checks; folder stale-tab conflict visibly reloads instead of overwriting | Implemented adaptation; shared data, no multi-device sync |
| About/diagnostics | Health-derived build version, reference version and Copy Build Info verified; existing timing/provenance diagnostics API tests | Implemented; no keys or prompt content added to diagnostics |

## Compatibility limits and retained exceptions

1. **Provider dynamics:** registered endpoints remain explicit; unsupported samplers
   remain unavailable with reasons. No engine management, model downloads or
   unattended loading. Budgets remain byte estimates, not tokenizer-exact counts.
   Compression is configured in Memory rather than per character. Native MathML
   has browser-dependent glyph metrics and supports KaTeX's bounded TeX subset;
   malformed/unsupported/oversized input falls back to source. It intentionally
   differs from upstream math HTML to preserve CSP. Highlighting covers the pinned
   common-language bundle; unknown/oversized blocks remain plain code. Attachments
   remain bounded UTF-8 text, not arbitrary binary document extraction.
2. **Security exclusions:** arbitrary JS/card hooks, unrestricted CSS/remote styles,
   installers, executable downloads, firewall/antivirus changes, engine controls,
   non-loopback binding and recursive uninstall remain excluded. Declarative MODS
   replaces executable extensions. No accepted decision changes.
3. **Persistence adaptation:** profiles are settings bundles in one shared database,
   not isolated data partitions. SQLite/revisions replace browser mirror and
   multi-device synchronization. Jobs resume by accumulated snapshots rather than
   persistent per-event replay; process restart marks unfinished work interrupted.
4. **Native snapshots:** interchange exports cards, threads, speakers, tree and
   organization. Endpoint/model settings, generation request provenance, summaries,
   macros/theme presets, schedules and profiles are not included. Credential-free
   SQLite backups preserve the broader database. A full interchange export is
   labeled “cards + threads” in the UI; it is not a full settings backup. Missing
   settings/schedule interchange remains a compatibility limitation, not an
   owner-approved waiver of parity.
5. **GobboNet import:** only the pinned `gobbonet_export` envelope version 1 and
   state schema absent/1 are accepted. Characters/personas, names, writing style,
   lore/storybook, greetings/carousel, supported samplers, cast identities, message
   variants/continuations, folder names, pins and tags are mapped. The pinned
   thread `name` field is supported. Unknown card fields remain bounded inert
   metadata. Remote/invalid/oversized assets are omitted with a report; no fetch.
   Settings, credentials, jobs, extensions, search data, attachments, summaries,
   timestamps and manual ordering do not activate/carry over. Missing bundled
   cast uses Assistant/You. Assistant variants receive copies of their shared
   continuation; user variants retain their own parked continuation. Nesting is
   capped at 64, 64 variants per message and expanded text at 64 MiB, in addition
   to existing file/card/conversation/message limits. CHARX sidecar modules beyond
   the main icon are still not mapped. These are reported compatibility limits.
6. **Approved UI adaptations/remaining checks:** explicit provider/privacy/model controls,
   cast selectors, memory tools, extra dashboard connection/profile sections and
   safe dialogs are covered by the owner’s visual approval for the current implementation. Memory lives in About and
   the active-thread chip; the sidebar has the pinned three footer pairs. Folder
   creation is beside search. Starter character text is Labbonet-authored.
   Per-schedule reply cap, folder-aware schedule picker and browser-closed desktop
   notifications are not implemented. Long live-model scrolling, touch drag,
   browser-closed/server-running real-provider schedules and busy/deleted-thread
   browser outcomes remain uncertified; relevant backend rules have tests.

## Browser evidence and visual comparison

Only throwaway databases were used on loopback ports 4850/4852/4854/4858/4860; no owner
database or inference server settings were changed. No live inference/search or
model-management request was made. Temporary viewport/cache overrides were reset.

| Capture | Scope |
|---|---|
| [Pinned shell, wide](evidence/m8-reference-wide.jpg), [420 px](evidence/m8-reference-panel.jpg) | Static pinned `chat.html` shell and CSS, event handlers removed, inline presentation moved to a fixture stylesheet; no upstream runtime or excluded code executed. Not a running GobboNet certification. |
| [Chat, wide](evidence/m8-chat-wide.jpg), [420 px](evidence/m8-chat-panel.jpg) | Imported test cast, math, highlighted code, file actions, model selector and composer; no document horizontal overflow at 1280/420 CSS px. |
| [Dashboard, wide](evidence/m8-home-wide.jpg), [420 px](evidence/m8-home-panel.jpg) | Pinned landing presentation with Labbonet data/starter/connection/profile integration. |
| [Folder conflict](evidence/m8-folder-conflict-wide.jpg) | Stale order refused and list reloaded; current order persisted across reload. |
| [Memory, 420 px](evidence/m8-memory-panel.jpg) | About → Memory works after removing the extra footer row. |
| [Skipped schedule, 420 px](evidence/m8-schedule-skipped-panel.jpg) | Explicit no-endpoint/model reason, no generation. “Finished generation attempts” intentionally excludes skipped/missed runs; their records remain visible in the ledger. |
| [Chrome renderer](evidence/m8-render-chrome.jpg) | **13 PASS, 0 FAIL, 0 CSP violations**, no style attributes/elements, no console warnings/errors. |
| [Embedded build](evidence/m8-embedded-wide.jpg) | Normal self-contained binary, no development frontend directory; real chooser import, MathML/highlighting and pinned assets rendered successfully. |

The in-app browser also showed 13 passes but reported a blocked injected inline
style, matching the earlier comments-harness observation. A stale development
cache briefly served an old `dialogs.js`; a clean-origin check passed, and the
server now sends `Cache-Control: no-cache` for unversioned assets with a regression
test. Chrome independently confirmed the final renderer/CSP result above.

The reference fixture checks shell styling and breakpoints; its welcome state is
not the same content as the populated Labbonet captures. It is **not** a pixel-diff
proof of all dialogs. The owner passed visual approval on 2026-10-18. This approval does not turn the
captures into an independent pixel-diff measurement or waive functional gaps.
Panel images use direct 420 × 900 captures after rejecting the browser service's
scaled-canvas screenshots during visual QA.

## Gates, owner approval and deferred work

Required gates: `gofmt -l .`, `go vet ./...`, `go test ./...`, normal build,
`CGO_ENABLED=0 go build ./...`, all frontend JS syntax checks and `git diff --check`.
Final results are recorded in `devlog.md` and `evidence/m8-verification.json`.

- Owner visual approval: **passed, 2026-10-18**, explicitly stated by the owner
  in chat ("Visual approval:passed"). Applies to the current implementation; does
  not waive functional gaps or grant Phase 1 sign-off.
- Full parity: **limited**, with the concrete exceptions/uncertified cases above.
- Real embedding/search/summarizer verification: **owner-deferred on 2026-10-18**,
  not passed or waived. No resumption date/version specified; resume when the owner
  requests it, subject to the existing loaded-model/credential controls.
- A-ST5/A-ST6/A-LM6, P3-P5 and final Phase 1 sign-off: **owner-deferred until
  V2-0-0**, unchanged; no waiver or pass recorded.
