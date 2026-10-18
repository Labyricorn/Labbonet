# GobboNet Reuse Log

Required by DR-003 (selective reuse) and DR-004 (AGPLv3-or-later). Add a row **before** a reused file or adapted component is merged.

## Rules

1. Copy only from the pinned commit in `GOBBONET-REFERENCE.md` (or log a new pin first).
2. Never copy anything listed in `GOBBONET-REFERENCE.md` section 3.
3. Keep the upstream copyright and license notice at the top of the reused file.
4. Add a change notice under it: `Modified by the Labbonet project, <date>: <summary>.`
5. Review the code against `docs/SECURITY-MODEL.md` before merging, and note the result in the Review column.

## Log

| Date | Labbonet path | Upstream path | Upstream commit | Kind (copy, adapt, partial) | Changes | Notices kept | Review |
|---|---|---|---|---|---|---|---|
| 2026-10-17 | `frontend/css/01-tokens.css` | `css/01-tokens.css` | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` | adapt | Exact palette, font stack, scanlines and animation; omit missing upstream font-face resource (font file is absent from the pinned tree) | Original header retained; copyright/license and modification notice added | Presentation only; no network/runtime hooks |
| 2026-10-17 | `frontend/css/02-layout.css`, `03-threads.css`, `04-chat.css`, `10-responsive.css` | corresponding `css/` paths | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` | copy | Original layout, controls, thread/message geometry and responsive rules; Labbonet integration overrides are separate | Original headers retained; copyright/license and modification notices added | CSS only; excluded behavior files not copied |
| 2026-10-17 | `frontend/css/components.css` | `css/13-components.css` | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` | partial | Header model selector and collapsible reasoning rules only | Source attribution, copyright/license and modification notice added | Pure styles; no extension hooks |
| 2026-10-17 | `frontend/css/search.css` | `css/12-organisation.css` | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` | partial | Original search-box geometry and clear button styles | Source attribution and modification notice added | Search presentation only; no deferred folder/tag code |
| 2026-10-17 | `frontend/index.html`, `frontend/js/conversations.js`, `frontend/js/chat.js` | `chat.html` (sidebar/chat/composer structure) | `314bdc9cae204ae38fdc1002ac6a9fb1437c109f` | adapt | Sidebar, header and message presentation mapped to existing safe Labbonet modules; no inline handlers/styles or provider-control operations | Copyright/license and change notice added | Only Phase 1 controls wired; no excluded JS/installer/engine code |

The records above cover the owner-approved visual adaptation. Labbonet is not affiliated with GobboNet or its authors.
