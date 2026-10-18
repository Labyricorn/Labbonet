# Frontend

Inherits `../AGENTS.md` and `G:\.vibe\AGENTS.md`.

## Purpose

Phase 1 browser interface and the owner-directed GobboNet presentation adaptation.

## Ownership

- `index.html`, `app.js`: shell, navigation, health and shared controls.
- `css/`: pinned GobboNet presentation files and extracted components, with reuse notices.
- `app.css`: Labbonet integration and provider-specific presentation.
- `js/`: API/SSE client, conversation/chat, models/endpoints, diagnostics and safe rendering.
- `vendor/`: pinned renderer/sanitizer, licenses and hash inventory.
- `tests/`: development-only render checks; not embedded.
- `embed.go`: shipped assets. Backend, storage, protocol and acceptance records remain parent-owned.

## Local Contracts

- Owner requirement: match the pinned GobboNet interface pixel for pixel except where model dynamics require differences. Functional tests do not constitute visual acceptance.
- Use only the pin in `../docs/GOBBONET-REFERENCE.md`. Log reuse in `../docs/REUSE-LOG.md` before copying; retain notices and mark changes. Never import excluded behavior code.
- Model output and reasoning go through `renderMarkdown`. No raw untrusted HTML, inline handlers/styles, eval, remote scripts or font loads. Keep the parent security model/CSP.
- Send through the backend API with its session/header controls. Keep endpoint/privacy/model state visible and retain the unloaded-model confirmation. Never perform provider model management from the browser.
- Deferred or excluded controls must not imply working features; currently CHARS/MODS/SCHED are disabled. Provider changes must finish before Send is enabled.

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
