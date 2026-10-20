# Instructions for Labyricorn publishing content

These instructions apply to the entire `.labyricorn/` directory. More specific
instructions in `project/AGENTS.md` and `devlog/AGENTS.md` also apply within
those directories.

## Purpose and ownership

- This directory is the repository-owned source for the project's exhibition
  page and development log on Labyricorn.
- This project repository is authoritative for the content. The Labyricorn site
  imports it as read-only content.
- Read `.labyricorn/README.md` and the nearest scoped `AGENTS.md` before editing
  a publishing record.
- Keep publishing changes focused. Do not alter application code merely to
  support an exhibition or devlog edit unless the user separately requests it.

## Authorization and content contract

- Preserve unrelated and uncommitted work. Never discard changes to obtain a
  clean working tree.
- Do not commit or push unless the user explicitly requests publication.
- Use native Lektor records named `contents.lr` and preserve `schema_version: 1`
  until a coordinated schema migration is approved.
- Keep attachments inside the record subtree that owns them.
- Use UTF-8 text, `YYYY-MM-DD` dates, and stable lowercase hyphenated slugs.
- Do not add templates, models, plugins, workflows, executable files, builds,
  application state, credentials, raw HTML, scripts, or active third-party
  content below `.labyricorn/`.
- Treat published URLs as durable. Ask before renaming or removing a published
  record; redirects or archival behavior may be required first.

## Security, synchronization, and review

- Never store or print passwords, API tokens, refresh credentials, private keys,
  `.netrc` contents, or other secrets in this directory.
- Do not claim content is public merely because a push succeeded. Report push
  status and Labyricorn synchronization status separately.
- Parse changed records, confirm referenced source commits exist, run
  `git diff --check`, and review the exact publishing diff before committing.
- At handoff, report changed publishing records, validation performed, commit
  and push status, and synchronization status if known.
