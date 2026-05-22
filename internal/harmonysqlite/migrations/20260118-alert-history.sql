-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20260118-alert-history.sql
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

-- Persistent alert history with acknowledgment support
-- Replaces the simple alerts table with a more comprehensive system

-- Create new alert_history table
CREATE TABLE IF NOT EXISTS alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_name VARCHAR(255) NOT NULL,          -- Alert category (e.g., 'WindowPost', 'WinningPost', 'NowCheck')
    message TEXT NOT NULL,                      -- Alert message content
    machine_name VARCHAR(255),                  -- Machine that generated the alert (for NowCheck alerts)
    created_at DATETIME DEFAULT NOW(),
    
    -- Acknowledgment fields
    acknowledged INTEGER DEFAULT FALSE,
    acknowledged_by VARCHAR(255),
    acknowledged_at DATETIME,
    
    -- Tracking
    sent_to_plugins INTEGER DEFAULT FALSE,      -- Whether alert was sent to external plugins
    sent_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_alert_history_created ON alert_history(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_unacked ON alert_history(acknowledged, created_at DESC) WHERE NOT acknowledged;
CREATE INDEX IF NOT EXISTS idx_alert_history_name ON alert_history(alert_name, created_at DESC);

-- Alert comments table
CREATE TABLE IF NOT EXISTS alert_comments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id INTEGER NOT NULL REFERENCES alert_history(id) ON DELETE CASCADE,
    comment TEXT NOT NULL,
    created_by VARCHAR(255) NOT NULL,
    created_at DATETIME DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alert_comments_alert ON alert_comments(alert_id);

-- (PG COMMENT ON stripped) COMMENT ON TABLE alert_history IS 'Persistent storage of all alerts with acknowledgment tracking';
-- (PG COMMENT ON stripped) COMMENT ON TABLE alert_comments IS 'Comments added to alerts by operators';
