# Frontend vendored dependencies

Vendored at pinned versions, no CDN loads at runtime (`SECURITY-MODEL.md` section 8, `ARCHITECTURE.md` section 8). Each file is loaded locally as a plain classic `<script>` (not a module), setting a `window` global the ES module code reads; neither library is fetched, built or bundled at runtime.

| File | Package | Version | License | SHA-256 |
|---|---|---|---|---|
| `markdown-it.min.js` | [markdown-it](https://github.com/markdown-it/markdown-it) | 14.1.0 | MIT (confirmed from the tagged `LICENSE` file, copied here as `markdown-it.LICENSE`) | `38c70a1e7ca91ab40e2d9e6e60129851a717ed1c7d4acbbdd41bf9503791cf68` |
| `purify.min.js` | [DOMPurify](https://github.com/cure53/DOMPurify) | 3.2.3 | Apache-2.0 or MPL-2.0, dual-licensed (confirmed from the tagged `LICENSE` file, copied here as `dompurify.LICENSE`) | `68769fa503bf609ec914fdbeba73eb1ea6fb4491292d0d0e691ea75aad570217` |

Both files are the upstream UMD build (unmodified, minified by upstream), downloaded on 2026-10-18 from `cdn.jsdelivr.net/npm/<package>@<version>/dist/<file>` (a one-time fetch to vendor the file, not a runtime CDN load) and verified by re-computing the SHA-256 above against the copy in this directory.

- `markdown-it.min.js` sets `window.markdownit` (a constructor function).
- `purify.min.js` sets `window.DOMPurify`.

`frontend/js/render.js` is the only file that reads these globals. To update either dependency: download the new version's `dist/<file>.min.js` and `LICENSE`, replace the files here, recompute the SHA-256, and update this table and the devlog entry recording the change.
