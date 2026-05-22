-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20250113-pdp-never-delete.sql
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

-- Add roots_added flag to track processing status
-- This aligns pdp_proofset_root_adds with pdp_proofset_creates behavior (never delete, only mark as processed)
ALTER TABLE pdp_proofset_root_adds ADD COLUMN IF NOT EXISTS roots_added INTEGER NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_pdp_proofset_root_adds_roots_added ON pdp_proofset_root_adds(roots_added);