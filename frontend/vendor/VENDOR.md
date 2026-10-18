# Frontend vendored dependencies

Vendored at pinned versions, no CDN loads at runtime (`SECURITY-MODEL.md` section 8, `ARCHITECTURE.md` section 8). Each file is loaded locally as a plain classic `<script>` (not a module), setting a `window` global the ES module code reads; neither library is fetched, built or bundled at runtime.

| File | Package | Version | License | SHA-256 |
|---|---|---|---|---|
| `markdown-it.min.js` | [markdown-it](https://github.com/markdown-it/markdown-it) | 14.1.0 | MIT (confirmed from the tagged `LICENSE` file, copied here as `markdown-it.LICENSE`) | `38c70a1e7ca91ab40e2d9e6e60129851a717ed1c7d4acbbdd41bf9503791cf68` |
| `purify.min.js` | [DOMPurify](https://github.com/cure53/DOMPurify) | 3.2.3 | Apache-2.0 or MPL-2.0, dual-licensed (confirmed from the tagged `LICENSE` file, copied here as `dompurify.LICENSE`) | `68769fa503bf609ec914fdbeba73eb1ea6fb4491292d0d0e691ea75aad570217` |

Both files are the upstream UMD build (unmodified, minified by upstream), downloaded on 2026-10-18 from `cdn.jsdelivr.net/npm/<package>@<version>/dist/<file>` (a one-time fetch to vendor the file, not a runtime CDN load) and verified by re-computing the SHA-256 above against the copy in this directory.

- `markdown-it.min.js` sets `window.markdownit` (a constructor function).
- `purify.min.js` sets `window.DOMPurify`.

`frontend/js/render.js` is the only application file that reads these globals. To update a dependency: download the pinned browser build and license, replace the files here, recompute SHA-256, and update this inventory and the devlog.

Milestone 8 additions (downloaded 2026-10-18, unmodified upstream browser builds):

| File | Package | Version | License | SHA-256 |
|---|---|---|---|---|
| `katex.min.js` | [KaTeX](https://github.com/KaTeX/KaTeX/tree/v0.16.22) | 0.16.22 | MIT; Khan Academy and contributors; tagged license in `katex.LICENSE` | `e8d885505949f3a5f4abdd5dd0d53696bd1371ad26ffbf4f310dcd77c8cdae89` |
| `highlight.min.js` | [highlight.js](https://github.com/highlightjs/highlight.js/tree/11.11.1) | 11.11.1 | BSD-3-Clause; Ivan Sagalaev; tagged license in `highlight.LICENSE` | `c4a399dd6f488bc97a3546e3476747b3e714c99c57b9473154c6fb8d259b9381` |

Build sources: `https://cdn.jsdelivr.net/npm/katex@0.16.22/dist/katex.min.js`
and `https://cdn.jsdelivr.net/gh/highlightjs/cdn-release@11.11.1/build/highlight.min.js`.
They set `window.katex` and `window.hljs`. KaTeX emits **MathML only** with trust
disabled, bounded expansion and size. No KaTeX CSS/fonts/auto-render, remote loads,
inline styles or trusted HTML commands are used. Highlighting uses explicit known
languages only, skips code over 32 KiB, and passes through DOMPurify with the whole
Markdown result. Source is retained for malformed/oversized math and unknown code.
