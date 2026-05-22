-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20260511-pdpv0-ipni-tracking.sql
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
-- Flagged constructs: DO $$ block
--

-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1
--         FROM information_schema.columns
--         WHERE table_name = 'pdp_piecerefs'
--           AND table_schema = current_schema()
--           AND column_name = 'indexed_at'
--     ) THEN
--         ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS indexed_at DATETIME DEFAULT NULL;
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
--         WHERE table_name = 'pdp_piecerefs'
--           AND table_schema = current_schema()
--           AND column_name = 'advertisement_created_at'
--     ) THEN
--         ALTER TABLE pdp_piecerefs ADD COLUMN IF NOT EXISTS advertisement_created_at DATETIME DEFAULT NULL;
--     END IF;
-- END
-- $$;


UPDATE pdp_piecerefs SET indexed_at = now(); -- This will create temporary inconsistency for rows which are not processed which will be resolved when they are processed.
UPDATE pdp_piecerefs SET advertisement_created_at = now(); -- This will create temporary inconsistency for rows which are not processed which will be resolved when they are processed.
