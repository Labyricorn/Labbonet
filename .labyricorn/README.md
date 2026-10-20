# Labyricorn publishing content

This directory is the repository-owned source for Labbonet's project
exhibition and development log on Labyricorn. The project repository remains
the source of truth; the Labyricorn site imports this content as read-only.

The content uses native Lektor records:

```text
.labyricorn/
├── AGENTS.md
├── README.md
├── project/
│   ├── AGENTS.md
│   ├── contents.lr
│   └── <optional image>
└── devlog/
    ├── AGENTS.md
    ├── contents.lr
    └── <entry-slug>/
        ├── contents.lr
        └── <optional images>
```

Rules:

- `project/contents.lr` uses the `project` model.
- `devlog/contents.lr` uses the `devlog` model.
- Each entry is a directory below `devlog/` containing a `contents.lr` record
  using the `devlog-entry` model.
- Entry directory names are stable public slugs. Do not rename a published entry
  without arranging a redirect on the Labyricorn site.
- Dates use `YYYY-MM-DD`; `source_commit` uses the full relevant commit ID.
- Publishing attachments stay inside the record subtree that owns them.
- Do not put credentials, builds, application state, or executable code here.

Editing these records does not itself prove that the public site synchronized.
Report repository publication and Labyricorn synchronization separately.
