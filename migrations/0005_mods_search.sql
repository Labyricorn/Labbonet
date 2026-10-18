-- Milestone 6: web search and MODS.
-- search_settings: one revisioned row (JSON settings) plus the DPAPI-protected API key.
--   The key is opaque to the store, never leaves it except through internal/secrets, and
--   is excluded from backups like endpoint credentials.
-- search_records: bounded per-generation record of what was searched and found (titles,
--   links, short snippets) or why the search failed; shown as citations.
-- macros: user-defined {{trigger}} expansions (trigger unique ignoring case).
-- appearance_settings: one revisioned row: the active preset and custom presets (JSON).
CREATE TABLE search_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    revision        INTEGER NOT NULL DEFAULT 1,
    data_json       TEXT NOT NULL DEFAULT '{}',
    credential_blob BLOB
);
INSERT INTO search_settings (id) VALUES (1);
CREATE TABLE search_records (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    generation_id   INTEGER,
    record_json     TEXT NOT NULL,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_search_records_conversation ON search_records(conversation_id, id);
CREATE TABLE macros (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL COLLATE NOCASE UNIQUE,
    body       TEXT NOT NULL,
    revision   INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
CREATE TABLE appearance_settings (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    revision  INTEGER NOT NULL DEFAULT 1,
    data_json TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO appearance_settings (id) VALUES (1);
