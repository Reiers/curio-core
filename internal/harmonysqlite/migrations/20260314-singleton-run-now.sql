-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20260314-singleton-run-now.sql
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

ALTER TABLE harmony_task_singletons ADD COLUMN IF NOT EXISTS run_now_request INTEGER NOT NULL DEFAULT FALSE;
