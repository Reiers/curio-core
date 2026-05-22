-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20240501-harmony-indexes.sql
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

-- Harmony counts failed tasks by task_id, without this index we'd do a full scan on the history table.
CREATE INDEX IF NOT EXISTS harmony_task_history_task_id_result_index
    ON harmony_task_history (task_id, result);

