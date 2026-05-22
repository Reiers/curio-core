-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20260110-pdp-v0-termination-handling.sql
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
-- Flagged constructs: DO $$ block, COMMENT ON
--

-- PDP Termination Handling
-- Tracks terminated datasets and applies backoff for contract reverts.

-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1
--         FROM information_schema.columns
--         WHERE table_name = 'pdp_data_sets'
--           AND table_schema = current_schema()
--           AND column_name = 'terminated_at_epoch'
--     ) THEN
--         ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS terminated_at_epoch BIGINT;
--     END IF;
-- END
-- $$;


-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1
--         FROM information_schema.columns
--         WHERE table_name = 'pdp_data_sets'
--           AND table_schema = current_schema()
--           AND column_name = 'consecutive_prove_failures'
--     ) THEN
--         ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS consecutive_prove_failures INT NOT NULL DEFAULT 0;
--     END IF;
-- END
-- $$;


-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1
--         FROM information_schema.columns
--         WHERE table_name = 'pdp_data_sets'
--           AND table_schema = current_schema()
--           AND column_name = 'next_prove_attempt_at'
--     ) THEN
--         ALTER TABLE pdp_data_sets ADD COLUMN IF NOT EXISTS next_prove_attempt_at BIGINT;
--     END IF;
-- END
-- $$;


-- (PG COMMENT ON stripped) COMMENT ON COLUMN pdp_data_sets.terminated_at_epoch IS 'Block height at which dataset termination was detected; NULL if active';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN pdp_data_sets.consecutive_prove_failures IS 'Number of consecutive proving failures (resets on success)';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN pdp_data_sets.next_prove_attempt_at IS 'Block height before which proving should not be attempted (backoff)';
