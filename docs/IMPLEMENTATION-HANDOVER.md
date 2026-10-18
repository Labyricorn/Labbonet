# Implementation handover — milestone 8 closeout

Updated 2026-10-18 (America/Los_Angeles). Supersedes the milestone 1–7 working-tree
handover. Development version: **V0-2-0**. The milestone 8 implementation/audit pass
is complete; full application parity and owner visual acceptance are not claimed.

## Current records

- `MILESTONE8-ACCEPTANCE.md`: authoritative inventory disposition, evidence,
  compatibility limits and remaining owner/provider/browser checks.
- `GOBBONET-PARITY-PLAN.md`: original authorized scope and milestone 8 additions.
- `ARCHITECTURE.md` section 16 and `SECURITY-MODEL.md` section 18: new boundaries.
- `devlog.md`: milestone implementation history and final gate results.
- `docs/evidence/m8-*`: current browser captures and machine-readable verification.

## Changes in this pass

- Vendored, licensed, hashed KaTeX/MathML and highlight.js; bounded input and safe
  source fallback through the existing sanitizer, without weakening CSP.
- Bounded pinned GobboNet export import (cards, cast, threads, variants/continuations,
  folders/pins/tags) with visible compatibility reports and no activated settings,
  credentials, schedules or extension code. Added all-thread export.
- Atomic complete-list folder reordering with stale-tab protection and real controls.
- In-app confirmation/notice dialogs. Dirty-card Keep/Escape/Discard and message
  deletion outcomes now work through these controls. Browser page-unload protection
  remains native. Unloaded-model confirmation still explicitly warns about loading.
- Pinned dashboard CSS/landing structure, original safe starter text, three sidebar
  footer pairs, compact folder-create control, About → Memory, version/build copy.
- Unversioned assets revalidate on reload, avoiding mixed-version module caches.

## Verification

Real browser chooser checks passed for portrait upload/preview/remove/reupload/save,
GobboNet DATA preview/commit and text attachments. Card export, bulk-thread download,
code Copy/Save, generated filename, Source/Rendered, profile deletion, starter install,
About/build Copy, Memory access, skipped schedule reason and folder reorder/reload/
two-tab 409 were exercised on disposable databases only. No live inference/search,
model management, owner database migration or browser permission change occurred.

Chrome: 13 render PASS, zero FAIL/CSP violations/style elements or attributes,
no console warnings/errors. The in-app browser independently had 13 passes but its
comments harness reported a blocked inline style; CSP was retained. Static pinned
shell/CSS and current app captures at 1280/420 CSS px support review, not pixel equality
of every dialog. Temporary overrides are reset. See the acceptance record for limits.

Final formatting/vet/full tests/normal and CGO-disabled builds/all JS syntax/diff
results are in `devlog.md` and `evidence/m8-verification.json`; rerun before code pushes.

## Remaining work

1. Owner visual acceptance and decisions on the explicit compatibility limitations in
   the acceptance record. They are not waivers. Do not call the application fully
   GobboNet-equivalent based on this pass.
2. Real embedding/search/summarizer and the listed long-stream/touch/browser-closed
   schedule checks remain uncertified. Use only already-loaded models and owner
   credentials; never operate model-management endpoints.
3. Snapshot settings/schedule/profile interchange, CHARX sidecar modules, exact token
   budgets, per-card compression model, isolated data profiles, per-schedule reply
   caps, folder-aware schedule picker and desktop notifications are still limited
   or absent as listed in the acceptance record. Credential-free database backups
   retain the broader database; interchange remains cards/threads only.
4. P3-P5, A-ST5/A-ST6/A-LM6 and final Phase 1 sign-off remain **owner-deferred until
   V2-0-0**, neither passed nor waived. This does not defer current visual acceptance.

## Working rules

Read the root/project/frontend DOX chain and devlog. Commit verified work directly
to main and push; no branches/PRs or delegated agents were used. Keep attribution,
provenance, notices and dependency inventories synchronized. Preserve `LICENSE`.
Pinned upstream remains `314bdc9cae204ae38fdc1002ac6a9fb1437c109f`.
