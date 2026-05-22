-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20230919-config.sql
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

CREATE TABLE IF NOT EXISTS harmony_config (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    title VARCHAR(300) UNIQUE NOT NULL,
    config TEXT NOT NULL
);