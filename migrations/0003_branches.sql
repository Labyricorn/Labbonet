-- Milestone 3: message tree (variants/branches), folders, tags, pins, manual order.
-- messages.parent_id already existed; it now defines the tree. selected_child_id
-- remembers the chosen variant below each message so each variant keeps its own
-- continuation. conversations.active_leaf_id is the tip of the visible branch and
-- the only path sent to a provider. conversations.revision guards tree edits.
CREATE TABLE folders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
ALTER TABLE conversations ADD COLUMN active_leaf_id INTEGER;
ALTER TABLE conversations ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN folder_id INTEGER REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE conversations ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
ALTER TABLE conversations ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE conversations ADD COLUMN forked_from_conversation_id INTEGER;
ALTER TABLE conversations ADD COLUMN forked_from_message_id INTEGER;
ALTER TABLE messages ADD COLUMN selected_child_id INTEGER;
ALTER TABLE messages ADD COLUMN origin TEXT NOT NULL DEFAULT 'original';
ALTER TABLE messages ADD COLUMN origin_id INTEGER;
CREATE TABLE conversation_tags (
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    tag             TEXT NOT NULL COLLATE NOCASE,
    PRIMARY KEY (conversation_id, tag)
);
CREATE INDEX idx_messages_parent ON messages(conversation_id, parent_id);
CREATE INDEX idx_conversations_folder ON conversations(folder_id);
-- Legacy linear history becomes a single branch: each message hangs below the
-- previous one in id order.
UPDATE messages SET parent_id = (SELECT MAX(p.id) FROM messages p WHERE p.conversation_id = messages.conversation_id AND p.id < messages.id) WHERE parent_id IS NULL;
UPDATE messages SET selected_child_id = (SELECT MIN(c.id) FROM messages c WHERE c.parent_id = messages.id);
UPDATE conversations SET active_leaf_id = (SELECT MAX(id) FROM messages WHERE conversation_id = conversations.id);
