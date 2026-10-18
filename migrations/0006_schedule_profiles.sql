-- Milestone 7: scheduler, run ledger and settings profiles.
-- schedules: one scheduled prompt per row. next_run is unix seconds (UTC) of the next due
--   time, NULL when disabled or finished. conversation_id becomes NULL if the thread is
--   deleted, so the schedule survives and reports why it cannot run.
-- schedule_runs: append-only ledger, newest 50 kept per schedule. For trigger 'schedule' the
--   unique (schedule_id, due_at) index makes a due time claimable once, however many ticks,
--   tabs or restarts see it. due_at is unix seconds.
-- profiles: named bundles of settings (appearance choice, memory settings, browser limits).
-- profile_state: the one active profile (informational; applying copies the values).
CREATE TABLE schedules (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL,
    conversation_id INTEGER REFERENCES conversations(id) ON DELETE SET NULL,
    prompt          TEXT NOT NULL,
    kind            TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    run_date        TEXT NOT NULL DEFAULT '',
    run_time        TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 0,
    web_search      INTEGER NOT NULL DEFAULT 0,
    auto_continue   INTEGER NOT NULL DEFAULT 0,
    next_run        INTEGER,
    revision        INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX idx_schedules_due ON schedules(enabled, next_run);
CREATE TABLE schedule_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    schedule_id     INTEGER NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    conversation_id INTEGER,
    trigger         TEXT NOT NULL,
    due_at          INTEGER NOT NULL,
    status          TEXT NOT NULL,
    reason          TEXT NOT NULL DEFAULT '',
    turns           INTEGER NOT NULL DEFAULT 0,
    generation_id   INTEGER,
    started_at      TEXT NOT NULL,
    finished_at     TEXT
);
CREATE INDEX idx_schedule_runs_schedule ON schedule_runs(schedule_id, id);
CREATE UNIQUE INDEX idx_schedule_runs_due ON schedule_runs(schedule_id, due_at) WHERE trigger = 'schedule';
CREATE TABLE profiles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL COLLATE NOCASE UNIQUE,
    data_json  TEXT NOT NULL,
    revision   INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
CREATE TABLE profile_state (
    id        INTEGER PRIMARY KEY CHECK (id = 1),
    active_id INTEGER REFERENCES profiles(id) ON DELETE SET NULL
);
INSERT INTO profile_state (id) VALUES (1);
