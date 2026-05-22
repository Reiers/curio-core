-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20251010-pdp-v0-fix-add-piece-constraints.sql
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
-- Flagged constructs: DO $$ block, DROP CONSTRAINT (limited in SQLite)
--

-- This file was update on 16th April 2026 to change the primary key from Yugabyte specific to Postgres style.
-- Any SP, which has already run this file, will never run again. So, new file 20260414-pdp-v0-fix-add-piece-constraints.sql
-- will fix the constraint for them if required. New SPs will get the correct constraint from here.
-- Note: This goes against best practices of never changing the already executed SQL files. This is the only exception.


-- changes an errand constraint from (data_set, add_message_hash, sub_piece_offset) to (data_set, add_message_hash, add_message_index)

-- TODO: PG-DROP-CONSTRAINT. SQLite can't drop a named constraint;
-- recreate the table without the constraint, or accept the old constraint.
-- ALTER TABLE pdp_data_set_piece_adds   DROP CONSTRAINT IF EXISTS pdp_data_set_piece_adds_piece_id_unique;

-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--   IF NOT EXISTS (
--     SELECT 1
--     FROM pg_constraint
--     WHERE conname = 'pdp_data_set_piece_adds_pk'
--       AND conrelid = to_regclass('pdp_data_set_piece_adds')
--   ) THEN
--     ALTER TABLE pdp_data_set_piece_adds
--       ADD CONSTRAINT pdp_data_set_piece_adds_pk
--       PRIMARY KEY (data_set, add_message_hash, add_message_index);
--   END IF;
-- END $$;
