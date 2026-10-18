-- Milestone 5: lore, context management and retrieval.
-- memory_settings: one revisioned row of retrieval/summary/embedding choices (JSON).
-- conversation_summaries: append-only revisions of the running summary. A revision
-- applies to a branch only when anchor_message_id is on that branch; through_message_id is
-- the last message it archived (NULL = nothing archived). Failed passes are recorded
-- (source 'failed') so gaps are reported, but never change what is archived.
-- embedding_cache: vectors keyed by content hash and exact model identity, so a changed
-- model or text can never reuse a stale vector.
-- retrieval_records: bounded per-generation telemetry for the inspector.
CREATE TABLE memory_settings (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    revision   INTEGER NOT NULL DEFAULT 1,
    data_json  TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO memory_settings (id) VALUES (1);
CREATE TABLE conversation_summaries (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id    INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    seq                INTEGER NOT NULL,
    text               TEXT NOT NULL,
    through_message_id INTEGER,
    anchor_message_id  INTEGER,
    archived_count     INTEGER NOT NULL DEFAULT 0,
    source             TEXT NOT NULL CHECK (source IN ('auto', 'manual', 'failed')),
    endpoint_id        INTEGER,
    model              TEXT NOT NULL DEFAULT '',
    note               TEXT NOT NULL DEFAULT '',
    created_at         TEXT NOT NULL,
    UNIQUE (conversation_id, seq)
);
CREATE TABLE embedding_cache (
    hash        TEXT NOT NULL,
    endpoint_id INTEGER NOT NULL,
    model_id    TEXT NOT NULL,
    dim         INTEGER NOT NULL,
    vec         BLOB NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (hash, endpoint_id, model_id)
);
CREATE TABLE retrieval_records (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    generation_id   INTEGER,
    record_json     TEXT NOT NULL,
    created_at      TEXT NOT NULL
);
CREATE INDEX idx_summaries_conversation ON conversation_summaries(conversation_id, seq);
CREATE INDEX idx_retrieval_conversation ON retrieval_records(conversation_id, id);
