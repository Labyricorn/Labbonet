# Frontend

Inherits `../AGENTS.md` and `G:\.vibe\AGENTS.md`.

## Purpose

Phase 1 browser interface and the owner-directed GobboNet presentation adaptation.

## Ownership

- `index.html`, `app.js`: shell, navigation, health and shared controls.
- `css/`: pinned GobboNet presentation files and extracted components, with reuse notices.
- `app.css`: Labbonet integration and provider-specific presentation.
- `js/`: API/SSE client, conversation sidebar (folders, pins, tags, drag order, content search), chat (message actions, variants, branch-aware generation), cast, DATA dialog (`data.js`), form dialogs (`dialogs.js`), chat tools (`tools.js`: code Copy/Save, source toggle, text attachments, reply limits, scroll follow), memory (`memory.js`: summary editor, retrieval inspector and settings dialog, header chip; the storybook field lives in `cast.js`), MODS and search (`mods.js`: macros, appearance presets, search settings, boot-time `bootAppearance`; `search.js`: composer opt-in box and reply citations), models/endpoints, diagnostics and safe rendering.
- `vendor/`: pinned renderer/sanitizer, licenses and hash inventory.
- `tests/`: development-only render checks; not embedded.
- `css/09-dashboard.css`: pinned landing-page presentation; `js/home.js` adapts its sections/tiles with safe DOM and original Labbonet starter text.
- `embed.go`: shipped assets. Backend, storage, protocol and acceptance records remain parent-owned.

## Local Contracts

- Owner requirement: match the pinned GobboNet interface pixel for pixel except where model dynamics require differences. Functional tests do not constitute visual acceptance. The owner explicitly passed visual approval for the current V0-2-0 implementation on 2026-10-18; preserve that approved presentation while completing functional work.
- Use only the pin in `../docs/GOBBONET-REFERENCE.md`. Log reuse in `../docs/REUSE-LOG.md` before copying; retain notices and mark changes. Never import excluded behavior code.
- Attribution: every reused visual element/structure and behavior reference must identify its GobboNet origin and Labbonet implementation boundary in `../docs/REUSE-LOG.md`; keep `../docs/GOBBONET-PROVENANCE.json` and `../NOTICE.md` consistent. Mixed frontend modules need explicit source/design vs runtime implementation notices.
- Model output and reasoning go through `renderMarkdown`. No raw untrusted HTML, inline handlers/styles, eval, remote scripts or font loads. Keep the parent security model/CSP.
- Math uses vendored KaTeX with MathML-only output, trust disabled, 8 KiB source/200 expansions/10 em size bounds and readable source fallback; the complete result is sanitized without style attributes. Explicit known-language highlighting skips code above 32 KiB and preserves exact text. No auto-detection, TeX HTML extensions, KaTeX HTML/font CSS or runtime CDN requests.
- Use `dialogs.js` for confirmations/notices. Discard/delete/model-load choices must retain their explicit text; Escape cancels. Card Close, Back and Escape must all protect unsaved edits; page-unload protection remains browser-native. MEMORY SETTINGS is in About and the active-thread chip; keep the three pinned sidebar footer pairs.
- DATA accepts bounded pinned GobboNet export envelopes with a visible compatibility report; imported settings/credentials/jobs/extensions never activate. All-thread export includes referenced cast. Folder-order requests send the complete expected and desired order and reload on 409.
- Send through the backend API with its session/header controls. Keep endpoint/privacy/model state visible and retain the unloaded-model confirmation. Never perform provider model management from the browser.
- Branch edits (edit, switch, delete) and sends echo the conversation `revision` and must reload on `409`; use `dialogs.js`, not `prompt()`, for text input. Message actions stay hidden while a reply streams.
- Replies are detached server jobs: aborting a stream only stops watching. Stop must go through the cancel API; a reply still running when a thread opens is re-attached (`attachGeneration`). Attachment, reply-cap and reasoning-limit values are range-checked by the server; per-viewer prefs live in `localStorage` guarded by try/catch. Attachment, code and message text are only ever set via `textContent`/Blob.
- Memory UI: summary edits echo the latest revision (`expected_seq`) and settings echo their revision; both reload on `409`. Server memory values (summaries, record ids, notes, model names) are only set with `textContent`; destructive clear is a timed two-click confirmation, not `confirm()`. `conversation-changed` and `memory-changed` document events drive the chip and dialog.
- MODS/search UI: appearance values are applied only through `style.setProperty`/`removeProperty` after the shape check in `applyAppearance` (names from the server allowlist, `#rrggbb` or a plain number); never add stylesheet text, URLs, `<link>`/`<style>`/`<script>` creation or a remote font/image. The search box is only enabled when the server reports search enabled with a key, must keep the boundary tooltip (text goes to the named service), and results/citations/macros/record text are set with `textContent`; citation links need an `http(s)` parse, `rel="noopener noreferrer"` and no referrer. Macro/appearance/search saves echo their revision and reload on `409`; unsaved appearance previews are reverted on close. The search key is write-only: the field is cleared after saving and never repopulated.
- Deferred or excluded controls must not imply working features; CHARS provides the cast library/editor, DATA provides backup/import/export/reset, and SCHED provides schedule CRUD, run history and Stop. Provider changes must finish before Send is enabled.
- `js/schedule.js` owns SCHED; `js/home.js` owns the dashboard and settings profiles. Schedules echo revisions, reload on 409, use two-click deletion and show both schedule-zone and local times. The server-running requirement and missed-run policy must stay visible. Profiles capture shared-database settings only; label this boundary. Apply maps server snake_case prefs to the existing browser camelCase prefs and refreshes appearance/memory. Home listeners and polling end when navigating to a thread; polling must not discard an active form. `open-thread`, `new-thread` and `schedules-changed` document events connect these screens to conversations.

## Work Guidance

- Keep upstream presentation rules in `css/` and integration overrides in `app.css`.
- Check wide and Codex side-panel/narrow layouts, including sidebar, thread search, model selector, reasoning and composer.
- The pinned tree lacks the font file referenced by upstream CSS; preserve its Cascadia/Consolas fallback rather than adding an unverified resource.

## Verification

- `node --check` for changed JS modules.
- Run the app with `--frontend-dir frontend`; `/tests/render.html` must show 13 passes and no console/CSP errors. Verify in Chrome independently if the in-app comments harness reports a blocked injected inline style; record both observations without weakening CSP.
- Exercise affected real controls in the browser and save evidence in `../docs/evidence/`. Restore temporary viewport overrides.
- Run the parent plan's Go formatting/vet/tests/build checks before pushing; embed additions must work in a normal build.

## Child DOX Index

None.
