-- Phase 1 initial schema (ARCHITECTURE.md section 5).

CREATE TABLE endpoints (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL,
    provider_type     TEXT NOT NULL,
    base_url          TEXT NOT NULL,
    privacy_class     TEXT NOT NULL,
    enabled           INTEGER NOT NULL DEFAULT 1,
    concurrency_limit INTEGER NOT NULL DEFAULT 1,
    detected_service  TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL
);

CREATE TABLE endpoint_credentials (
    endpoint_id    INTEGER PRIMARY KEY REFERENCES endpoints(id) ON DELETE CASCADE,
    protected_blob BLOB NOT NULL,
    created_at     TEXT NOT NULL
);

CREATE TABLE models (
    endpoint_id       INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    model_id          TEXT NOT NULL,
    type              TEXT NOT NULL,
    state             TEXT NOT NULL,
    context_length    INTEGER NOT NULL DEFAULT 0,
    capabilities_json TEXT NOT NULL DEFAULT '{}',
    seen_at           TEXT NOT NULL,
    PRIMARY KEY (endpoint_id, model_id)
);

CREATE TABLE conversations (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    title         TEXT NOT NULL,
    endpoint_id   INTEGER REFERENCES endpoints(id),
    model_id      TEXT,
    system_prompt TEXT NOT NULL DEFAULT '',
    params_json   TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    parent_id       INTEGER REFERENCES messages(id),
    role            TEXT NOT NULL,
    content         TEXT NOT NULL,
    reasoning       TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE TABLE generations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id      INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    endpoint_id     INTEGER REFERENCES endpoints(id),
    model_requested TEXT NOT NULL,
    model_reported  TEXT NOT NULL DEFAULT '',
    params_json     TEXT NOT NULL DEFAULT '{}',
    started_at      TEXT NOT NULL,
    first_token_at  TEXT,
    finished_at     TEXT,
    finish_reason   TEXT NOT NULL DEFAULT '',
    usage_json      TEXT NOT NULL DEFAULT '{}',
    timings_json    TEXT NOT NULL DEFAULT '{}',
    error_kind      TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX idx_generations_message_id ON generations(message_id);
CREATE INDEX idx_models_endpoint_id ON models(endpoint_id);
CREATE INDEX idx_conversations_updated_at ON conversations(updated_at);
