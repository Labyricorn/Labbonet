# Implementation handover — current state

Updated 2026-10-18 (America/Los_Angeles). Supersedes the 2026-10-17 partial-backend handover. The original owner-authorized eight-milestone parity scope remains; milestone 1 is in progress, milestones 2–8 are untouched. Phase 1, deferred provider windows and owner visual acceptance remain open.

## Current working tree

The inherited backend WIP is retained and refined: schema 2, revisioned cast store/API, defaults frozen at conversation creation, immutable message speakers, bounded inert prompt assembly, transactional turn creation and pre-upgrade credential-free backup. CHARS is now wired: safe native-dialog character/persona library/editor, CRUD/copy/atomic order/defaults, text fields/macros/greeting/carousel/presets, local raster input/preview/remove, persisted per-thread cast and explicit greeting. History uses saved speaker names/portraits/colors; colors use validated CSSOM and safe quote spans. Unsupported provider samplers show a reason. No CSP, provider allowlist or model-management relaxation.

Important fixes: finite numeric validation; full raster decoding after bounds; bounded macro builder; greeting persona macros; pinned persona cadence (0 disabled, first turn and multiples of N at tail); same-conversation history/revision/streaming conflict check; cancel registration before start callback; strict atomic card reorder; generic unexpected storage errors. Legacy conversations initialize with no cast. Prompt provenance omits embedded images/unused greetings; message portraits remain historical snapshots.

The owner's additional request to document **every** reused GobboNet element is implemented in `docs/REUSE-LOG.md`, `docs/GOBBONET-PROVENANCE.json`, root `NOTICE.md`, README/About and per-file notices. Nine CSS files were compared against exact pinned source; equal spans and source hashes are recorded. Structure/behavior references are distinguished from copied source and original Labbonet runtime. Attribution is now a durable AGENTS contract. Check Git status/log for which documentation is committed; unfinished implementation must not be discarded.

## Verification completed

- Baseline and subsequent `go test ./...` pass; Go formatting/vet, normal build and CGO-disabled build pass. Changed JS passes `node --check`. Re-run mandatory gates after any code change before a code push.
- Dedicated cast/prompt/store/API/generation tests cover two characters/two personas after reopen, optimistic conflicts/wrong kinds/delete/defaults/history identity, atomic reorder rollback, populated v1 upgrade with live encrypted credential preservation and redacted backup, failed migration rollback, invalid prompts/assets/controls without new turns, correct fake-provider prompt/params, queued-edit snapshot and immediate cancel.
- Browser on separate temp database at `127.0.0.1:4817`: create/copy/default/reorder, per-thread cast switch, explicit expanded greeting, old speaker after switch and reload, text/dialog computed colors, no inline styles or CSP errors, two-tab stale editor conflict, five S14 passes. Saved evidence: `cast-library-wide.png`, `cast-library-panel.png`, `cast-chat-wide.png`, `gobbonet-attribution-about.png`.
- No new live inference requests; no provider settings/model operations; no owner database migration.

## Milestone 1 closeout still required

1. Finish browser file upload/preview/remove walkthrough. The Chrome extension refused chooser upload because “Allow access to file URLs” is off. This permission was not changed; do not bypass that refusal. Domain tests cover valid/truncated/unsafe rasters, but do not claim the UI upload path was verified.
2. Finish reliable dirty-editor close/Escape/navigation walkthrough. Escape emitted the correct discard confirmation, but browser handling stalled and retain/dismiss outcomes were not established. Existing native confirmation should be checked with a reliable UI route.
3. Complete live browser generation with saved cast if useful, using only an already-loaded model; automated fake-provider request/snapshot verification already passes. Do not operate deferred auth/cold-start windows or load an unloaded LM Studio model.
4. Review remaining UI refinements, exact pinned full-window/panel visual comparison and notices; owner visual acceptance remains separate. Check frontend errors on intentional conflict and after reload. Review context estimator limitations and snapshot storage growth (historical portraits still duplicate per message).
5. Update the acceptance/proposal inventory and this handover when milestone 1 is actually complete; then commit/push tested code on main per plan. Continue milestones 2–8 in order. Do not infer full application parity from CHARS.

## Workspace / governing rules

Read `G:\.vibe\AGENTS.md`, project `AGENTS.md`, root `devlog.md`, `frontend/AGENTS.md`, parity plan and reuse inventory before edits. No branches or PRs unless requested. No agents were delegated. Preserve the entire existing working tree. Exact upstream pin remains `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` (GobboNet 1.7.6). Upstream read-only comparison files are in `%TEMP%\labbonet-gobbonet-pin`, not a second source of truth.
