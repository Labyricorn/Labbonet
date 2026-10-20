# Instructions for the Labyricorn development log

These instructions apply to `.labyricorn/devlog/` and help coding assistants
maintain the project's public development narrative safely.

## Format and ownership

- Keep the devlog index at `devlog/contents.lr` using `_model: devlog` and
  `schema_version: 1`.
- Store each entry at `devlog/<stable-slug>/contents.lr` using
  `_model: devlog-entry` and `schema_version: 1`.
- Use lowercase hyphenated entry slugs. Published slugs are durable public URLs;
  do not rename or delete them without explicit approval and a redirect or
  archival decision.
- Every entry must contain `title`, `date`, `author`, `summary`, `tags`,
  `source_commit`, and Markdown `body` fields.
- Keep entry attachments in that entry's directory. Only PNG, JPEG, and WebP
  images are accepted by the Labyricorn importer.
- Do not add templates, models, plugins, executable code, builds, application
  state, credentials, or active third-party content below this directory.

## Content guidance

- Base entries on verifiable repository history, project documentation, and
  implemented behavior. Do not invent motivations, results, release status,
  user feedback, or completion claims.
- Explain the milestone, why it mattered, and what changed. Keep one main
  milestone per entry and write a summary that works in a chronological list.
  Do not merely expand a commit message or enumerate every changed file.
- Distinguish design and schema contracts from completed runtime behavior.
- Link to the relevant canonical commit using the repository's HTTPS web URL.
- Use the referenced commit's actual `YYYY-MM-DD` calendar date for retrospective
  entries, not the date on which the prose was drafted.
- Set `source_commit` to the full lowercase 40-character commit ID most directly
  associated with the milestone. It must be an ancestor of the published branch.
- Topics are comma-separated display labels. Unknown topics are allowed: the
  site keeps them visible but unlinked. Do not create or edit the site's tracked
  tag registry from this project repository.
- Use Markdown without raw HTML, scripts, embedded credentials, or active
  external content.
- Multiple entries may share a date. The site resolves ties from source history;
  do not invent timestamps or manual next/previous links.
- Do not rewrite older entries merely because the implementation later changed.
  Add a new entry or a clearly labelled correction when historical context is
  needed.

## Safety and review

- Preserve unrelated project files and uncommitted work. The devlog owns only
  `.labyricorn/devlog/`.
- Do not commit or push unless the user explicitly requests publication.
- Before publication, verify claims against the referenced commit, validate all
  records, check that the source-commit link uses the same full commit ID, review
  the exact devlog diff, and confirm unrelated files are not staged in the
  publishing commit.
- Prefer the repository-root `devlog_editor.py` for routine validation and safe
  publication when it is available.
