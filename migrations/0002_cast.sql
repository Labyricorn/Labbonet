CREATE TABLE cast_cards (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('character', 'persona')),
    revision INTEGER NOT NULL DEFAULT 1,
    position INTEGER NOT NULL DEFAULT 0,
    data_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE cast_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    revision INTEGER NOT NULL DEFAULT 1,
    character_id INTEGER REFERENCES cast_cards(id) ON DELETE SET NULL,
    persona_id INTEGER REFERENCES cast_cards(id) ON DELETE SET NULL
);
INSERT INTO cast_settings (id) VALUES (1);
CREATE TABLE conversation_cast (
    conversation_id INTEGER PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 1,
    character_id INTEGER REFERENCES cast_cards(id) ON DELETE SET NULL,
    persona_id INTEGER REFERENCES cast_cards(id) ON DELETE SET NULL,
    greeting INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE messages ADD COLUMN speaker_json TEXT NOT NULL DEFAULT '{}';
