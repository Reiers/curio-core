-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20240317-web-summary-index.sql
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

/* Used for webui clusterMachineSummary */
-- NOTE: This index is changed in 20240420-web-task-indexes.sql
CREATE INDEX IF NOT EXISTS harmony_task_history_work_index
	ON harmony_task_history (completed_by_host_and_port ASC, name ASC, result ASC, work_end DESC);

/* Used for webui actorSummary sp wins */
CREATE INDEX IF NOT EXISTS mining_tasks_won_sp_id_base_compute_time_index
    ON mining_tasks (won ASC, sp_id ASC, base_compute_time DESC);
