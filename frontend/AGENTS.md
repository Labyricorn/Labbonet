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
- `embed.go`: shipped assets. Backend, storage, protocol and acceptance records remain parent-owned.

## Local Contracts

- Owner requirement: match the pinned GobboNet interface pixel for pixel except where model dynamics require differences. Functional tests do not constitute visual acceptance.
- Use only the pin in `../docs/GOBBONET-REFERENCE.md`. Log reuse in `../docs/REUSE-LOG.md` before copying; retain notices and mark changes. Never import excluded behavior code.
- Attribution: every reused visual element/structure and behavior reference must identify its GobboNet origin and Labbonet implementation boundary in `../docs/REUSE-LOG.md`; keep `../docs/GOBBONET-PROVENANCE.json` and `../NOTICE.md` consistent. Mixed frontend modules need explicit source/design vs runtime implementation notices.
- Model output and reasoning go through `renderMarkdown`. No raw untrusted HTML, inline handlers/styles, eval, remote scripts or font loads. Keep the parent security model/CSP.
- Send through the backend API with its session/header controls. Keep endpoint/privacy/model state visible and retain the unloaded-model confirmation. Never perform provider model management from the browser.
- Branch edits (edit, switch, delete) and sends echo the conversation `revision` and must reload on `409`; use `dialogs.js`, not `prompt()`, for text input. Message actions stay hidden while a reply streams.
- Replies are detached server jobs: aborting a stream only stops watching. Stop must go through the cancel API; a reply still running when a thread opens is re-attached (`attachGeneration`). Attachment, reply-cap and reasoning-limit values are range-checked by the server; per-viewer prefs live in `localStorage` guarded by try/catch. Attachment, code and message text are only ever set via `textContent`/Blob.
- Memory UI: summary edits echo the latest revision (`expected_seq`) and settings echo their revision; both reload on `409`. Server memory values (summaries, record ids, notes, model names) are only set with `textContent`; destructive clear is a timed two-click confirmation, not `confirm()`. `conversation-changed` and `memory-changed` document events drive the chip and dialog.
- MODS/search UI: appearance values are applied only through `style.setProperty`/`removeProperty` after the shape check in `applyAppearance` (names from the server allowlist, `#rrggbb` or a plain number); never add stylesheet text, URLs, `<link>`/`<style>`/`<script>` creation or a remote font/image. The search box is only enabled when the server reports search enabled with a key, must keep the boundary tooltip (text goes to the named service), and results/citations/macros/record text are set with `textContent`; citation links need an `http(s)` parse, `rel="noopener noreferrer"` and no referrer. Macro/appearance/search saves echo their revision and reload on `409`; unsaved appearance previews are reverted on close. The search key is write-only: the field is cleared after saving and never repopulated.
- Deferred or excluded controls must not imply working features; currently SCHED is disabled; CHARS provides the cast library/editor and DATA provides backup, import/export and typed-confirmation reset. Provider changes must finish before Send is enabled.

## Work Guidance

- Keep upstream presentation rules in `css/` and integration overrides in `app.css`.
- Check wide and Codex side-panel/narrow layouts, including sidebar, thread search, model selector, reasoning and composer.
- The pinned tree lacks the font file referenced by upstream CSS; preserve its Cascadia/Consolas fallback rather than adding an unverified resource.

## Verification

- `node --check` for changed JS modules.
- Run the app with `--frontend-dir frontend`; `/tests/render.html` must show five passes and no console/CSP errors.
- Exercise affected real controls in the browser and save evidence in `../docs/evidence/`. Restore temporary viewport overrides.
- Run the parent plan's Go formatting/vet/tests/build checks before pushing; embed additions must work in a normal build.

## Child DOX Index

None.
