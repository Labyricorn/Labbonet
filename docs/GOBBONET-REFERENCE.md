# GobboNet Reference

**Version:** 0.1 (2026-10-18)
**Purpose:** pins the GobboNet revision used as Labbonet's feature and design reference (DR-003 acceptance criterion "GobboNet reference commit recorded"), and maps its source areas for selective reuse.

Labbonet is not affiliated with or endorsed by GobboNet or its authors.

---

## 1. Pinned reference

| Field | Value |
|---|---|
| Repository | https://github.com/ElodineOfficial/GobboNet |
| Branch | `main` (default) |
| Commit | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` |
| Commit date | 2026-09-24T19:28:37Z ("Update .gitignore") |
| Version file | `VERSION` = `1.7.6` |
| License | GNU AGPLv3-or-later; `LICENSE` header reads "Copyright (C) 2026 Elodine / GoblinCorps". GitHub's license detector reports `NOASSERTION`, so rely on the file text. |
| Language | Go backend (`go 1.25.0`, module `github.com/ElodineOfficial/GobboNet`), plain JS and CSS frontend |
| Recorded | 2026-10-18, read-only GitHub API |

The repository was last pushed 2026-10-15, later than this commit's date. Reuse and feature comparisons use the pinned commit until it is deliberately moved; moving it is logged in the decision register.

## 2. Source map at the pinned commit

The **Treatment** column is a starting point for review, not a reuse approval. Every reused file still goes through `REUSE-LOG.md`.

| Area | Paths | Treatment |
|---|---|---|
| Atomic file writes | `internal/atomicfile` | Reuse candidate (small, generic) |
| Session and login | `internal/auth` (`session.go`, `secret.go`, `ratelimit.go`, `login.go`) | Review; Labbonet's session model differs (`SECURITY-MODEL.md` section 4) |
| HTTP helpers | `internal/httpx`, `internal/server`, `internal/proxy` | Review for ideas; GobboNet proxies to its own engine |
| Jobs | `internal/jobs` | Review for Phase 4/5 background jobs |
| Store locking, state | `internal/storelock`, `internal/state` | Review for the single-instance lock |
| Keyring | `internal/keyring` (envelope, passphrase, wordlist) | Review; Labbonet uses DPAPI instead |
| Frontend render | `js/12-render.js`, `css/16-math.css`, `css/17-markdown.css` | Review for Markdown and LaTeX rendering; must pass Labbonet's sanitizer rules |
| Frontend chat, threads, scroll | `js/03-generation.js`, `js/09-threads.js`, `js/10-chat.js`, `js/14-scroll.js`, `css/02`–`css/04` | Feature reference; reuse candidates for Phase 1 UI |
| Prompt building | `js/07-prompt.js` | Feature reference (Phase 3) |
| RAG | `js/08-rag.js`, `docs/RAG_INFO.md` | Feature reference (Phase 4) |
| Cards, personas | `js/15-cards.js`, `js/16-card-io.js`, `js/17-personas.js`, `default-characters.json` | Feature reference (Phase 3) |
| Search, macros, scheduler | `js/11-search.js`, `js/20-macros.js`, `js/22-scheduler.js` | Feature reference (Phase 6) |

## 3. Excluded from reuse

These implement behaviors Labbonet excludes (AGENTS.md local contracts). Do not copy them or code derived from them.

| Behavior | Paths |
|---|---|
| Installers and packaging scripts | `installer`, `installer-win`, `installer-linux`, `installer-fedora`, `build-release.sh`, `make-zip.sh`, `stage-web.sh`, `verify-payload.sh` |
| PowerShell | `fileserver.ps1`, `hardware-probe.ps1`, `hw-recommend.ps1`, `identify-model.ps1` |
| LAN and firewall setup | `setup-lan.bat`, `teardown-lan.bat`, `cmd/gobbonet/doctor.go` firewall checks |
| Uninstall with recursive delete | `cmd/gobbonet/uninstall.go` |
| Autostart | `internal/autostart`, `cmd/gobbonet/autostart.go` |
| Local engine management | `internal/engine`, `internal/supervisor`, `cmd/gobbonet/engine.go`, `engine.sha256` |
| Model download and catalogue | `internal/modelfetch`, `internal/catalog`, `internal/models` |
| Hardware and config tuning for the engine | `internal/config` (perf, GPU layers), `internal/setup` |
| Unrestricted JavaScript hooks | `js/19-extensions.js`, `js/23-card-code.js`, `css/14-card-code.css` |

## 4. Feature-parity inventory

The 2026-10-17 owner-directed visual adaptation reuses only presentation CSS and selected HTML structure from this pin. Exact file/component mappings, independent-implementation boundaries and notices are in `REUSE-LOG.md`; `GOBBONET-PROVENANCE.json` records audited pinned-source hashes and equal CSS spans, and root `NOTICE.md` supplies public credit. The working Phase 1 preview is available for comparison; no claim of completed pixel equality or owner visual acceptance is implied by functional test results.

The feature list and its treatment live in `Initial_Project_Proposal.md` section 19. When a feature is implemented, mark it there with the Labbonet phase; keep implementation details out of that table (DR-003 criterion "feature-parity inventory maintained separately").
