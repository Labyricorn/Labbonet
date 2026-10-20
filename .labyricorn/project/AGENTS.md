# Instructions for the Labyricorn project record

These instructions supplement `.labyricorn/AGENTS.md` and apply to the
`project/` directory.

## Record structure

- Maintain exactly one project record at `project/contents.lr` using
  `_model: project` and `schema_version: 1`.
- Keep `project_id` stable. It is the durable identity used by the importer and
  public URL.
- Ask before changing the repository URL, default branch, project status,
  author, start date, or project identity.
- Store project-owned images in this directory and reference them by filename
  from `contents.lr`. Only PNG, JPEG, and WebP images are accepted.

## Editorial guidance

- Write the exhibition page as a concise project narrative, not as a copy of
  the repository README or as release documentation.
- Verify technical and status claims against the repository before editing.
- Distinguish completed work, formal design work, experiments, and future work.
- Do not manually add repository-derived commit, license, language, release,
  issue, star, or fork metadata. The Labyricorn importer owns those values.
- Keep the summary suitable for listings and social previews.

## Review requirements

- Confirm all required project fields are present and any image reference stays
  inside `project/`.
- Check Markdown links are intentional HTTPS links and prose makes no unsupported
  claims.
- Validate the record and run `git diff --check` before committing.
