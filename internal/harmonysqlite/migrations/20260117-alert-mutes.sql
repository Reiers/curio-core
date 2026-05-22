-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20260117-alert-mutes.sql
-- Translation pass: 2026-05-23 (Day 3 scaffolding).
--
-- Bulk substitutions applied:
--   SERIAL/BIGSERIAL PRIMARY KEY -> INTEGER PRIMARY KEY AUTOINCREMENT
--   TIMESTAMP[TZ] -> DATETIME
--   BOOLEAN -> INTEGER, TRUE/FALSE -> 1/0
--   BYTEA -> BLOB
--   JSONB -> TEXT (JSON-serialized at the Go layer)
--   <type>[] -> TEXT (JSON-encoded at the Go layer)
--   UUID -> TEXT, FLOAT -> REAL
--   NOW()/TIMEZONE('UTC',NOW())/CURRENT_TIMESTAMP AT TIME ZONE 'UTC' -> CURRENT_TIMESTAMP
--
-- TODO (manual): the source file contained PG-specific constructs that
-- can't be auto-translated 1:1. Search for `-- TODO: PG-` markers below.
-- Flagged constructs: COMMENT ON
--

-- Alert muting mechanism
-- Allows operators to mute specific alerts by pattern

CREATE TABLE IF NOT EXISTS alert_mutes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_name VARCHAR(255) NOT NULL,           -- Name of the alert category (e.g., 'WindowPost', 'WinningPost', 'NowCheck')
    pattern TEXT,                                -- Optional pattern to match within alert message (SQL LIKE pattern)
    reason TEXT NOT NULL,                        -- Reason for muting
    muted_by VARCHAR(255) NOT NULL,              -- Who muted this
    muted_at DATETIME DEFAULT NOW(),
    expires_at DATETIME,         -- NULL means never expires
    active INTEGER DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_alert_mutes_active ON alert_mutes(active, alert_name);

-- (PG COMMENT ON stripped) COMMENT ON TABLE alert_mutes IS 'Stores muted alert patterns to suppress specific alerts';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN alert_mutes.pattern IS 'SQL LIKE pattern to match alert messages. NULL means mute entire alert category.';
