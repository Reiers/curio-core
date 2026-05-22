-- Miscellaneous infra tables that don't fit into a larger group but are
-- used by Curio Core's harmonytask, HTTP server, and alert plumbing.
--
-- Source files folded in:
--   20240730-alerts.sql                  alerts table
--   20240906-http-server.sql             autocert_cache
--   20240927-task-retrywait.sql          ALTER harmony_task ADD retries
--   20241105-walletnames.sql             wallet_names
--   20241104-piece-info.sql              piece_summary (single-row counters)
--   20250111-machine-maintenance.sql     harmony_machines.unschedulable
--   20250129-msgwait-idx.sql             partial index on message_waits
--   20250422-msg-wait-timestamp.sql      message_waits.created_at
--   20250818-restart-request.sql         harmony_machines.restart_request
--   20250926-harmony_config_timestamp.sql harmony_config.timestamp
--   20260117-alert-mutes.sql             alert_mutes
--   20260118-alert-history.sql           alert_history + alert_comments
--   20260215-config-history.sql          harmony_config_history
--   20260314-singleton-run-now.sql       harmony_task_singletons.run_now_request
--   20260430-harmony-task-history-idx.sql harmony_task_history index
--   20260501-machine-detail-version.sql  harmony_machine_details.version
--
-- All folded into single CREATE TABLE / CREATE INDEX statements where
-- possible (avoids the SQLite ALTER COLUMN limitations).

CREATE TABLE IF NOT EXISTS alerts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    machine_name TEXT NOT NULL,
    message      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS autocert_cache (
    k TEXT NOT NULL PRIMARY KEY,
    v BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS wallet_names (
    wallet TEXT PRIMARY KEY,
    name   TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS piece_summary (
    id           INTEGER PRIMARY KEY CHECK (id = 1),     -- single-row sentinel
    total        INTEGER NOT NULL DEFAULT 0,
    indexed      INTEGER NOT NULL DEFAULT 0,
    announced    INTEGER NOT NULL DEFAULT 0,
    last_updated TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS alert_mutes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_name  TEXT NOT NULL,
    pattern     TEXT,
    reason      TEXT NOT NULL,
    muted_by    TEXT NOT NULL,
    muted_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TEXT,
    active      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_alert_mutes_active ON alert_mutes(active, alert_name);

CREATE TABLE IF NOT EXISTS alert_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_name    TEXT NOT NULL,
    message       TEXT NOT NULL,
    machine_name  TEXT,
    created_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    acknowledged       INTEGER NOT NULL DEFAULT 0,
    acknowledged_by    TEXT,
    acknowledged_at    TEXT,

    sent_to_plugins INTEGER NOT NULL DEFAULT 0,
    sent_at         TEXT
);
CREATE INDEX IF NOT EXISTS idx_alert_history_created  ON alert_history(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_alert_history_unacked  ON alert_history(acknowledged, created_at DESC) WHERE NOT acknowledged;
CREATE INDEX IF NOT EXISTS idx_alert_history_name     ON alert_history(alert_name, created_at DESC);

CREATE TABLE IF NOT EXISTS alert_comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    alert_id   INTEGER NOT NULL REFERENCES alert_history(id) ON DELETE CASCADE,
    comment    TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alert_comments_alert ON alert_comments(alert_id);

CREATE TABLE IF NOT EXISTS harmony_config_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL,
    config     TEXT NOT NULL,
    changed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Additions to tables defined in earlier migrations. SQLite supports
-- ALTER TABLE ADD COLUMN, so we can do these incrementally without a
-- table rebuild.

-- harmony_task additions: retries (20240927).
ALTER TABLE harmony_task ADD COLUMN retries INTEGER NOT NULL DEFAULT 0;

-- harmony_machines additions:
--   unschedulable (20250111), restart_request (20250818).
ALTER TABLE harmony_machines ADD COLUMN unschedulable INTEGER NOT NULL DEFAULT 0;
ALTER TABLE harmony_machines ADD COLUMN restart_request TEXT;
CREATE INDEX IF NOT EXISTS idx_harmony_machines_unschedulable
    ON harmony_machines (unschedulable);

-- message_waits additions: created_at (20250422) + partial index (20250129).
ALTER TABLE message_waits ADD COLUMN created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;
CREATE INDEX IF NOT EXISTS idx_message_waits_nulls
    ON message_waits (waiter_machine_id)
    WHERE waiter_machine_id IS NULL AND executed_tsk_cid IS NULL;
CREATE INDEX IF NOT EXISTS idx_message_waits_created_at_executed
    ON message_waits (created_at)
    WHERE executed_tsk_cid IS NOT NULL;

-- harmony_config additions: timestamp (20250926).
ALTER TABLE harmony_config ADD COLUMN timestamp TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP;

-- harmony_task_singletons additions: run_now_request (20260314).
ALTER TABLE harmony_task_singletons ADD COLUMN run_now_request INTEGER NOT NULL DEFAULT 0;

-- harmony_machine_details additions: version (20260501).
ALTER TABLE harmony_machine_details ADD COLUMN version TEXT;

-- harmony_task_history completed_by_host_and_port column from 20231113.
-- Must come BEFORE the index below that references it.
ALTER TABLE harmony_task_history ADD COLUMN completed_by_host_and_port TEXT NOT NULL DEFAULT '';

-- harmony_task_history index from 20240317 (the better version from 20240420).
CREATE INDEX IF NOT EXISTS harmony_task_history_work_index
    ON harmony_task_history (completed_by_host_and_port, name, result, work_end DESC);

-- harmony_task_history index from 20260430 (added later).
CREATE INDEX IF NOT EXISTS harmony_task_history_recent_task_result_idx
    ON harmony_task_history (work_end DESC, name, task_id, result);

-- harmony_task_history task_id+result index from 20240501.
CREATE INDEX IF NOT EXISTS harmony_task_history_task_id_result_index
    ON harmony_task_history (task_id, result);
