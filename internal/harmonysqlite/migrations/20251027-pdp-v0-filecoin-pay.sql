-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20251027-pdp-v0-filecoin-pay.sql
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
-- Flagged constructs: DO $$ block, ARRAY types (mapped to TEXT/JSON)
--

CREATE TABLE IF NOT EXISTS filecoin_payment_transactions (
    tx_hash TEXT PRIMARY KEY,
    rail_ids TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pdp_delete_data_set (
    id BIGINT PRIMARY KEY,

    terminate_service_task_id BIGINT,
    after_terminate_service INTEGER NOT NULL DEFAULT FALSE,
    terminate_tx_hash TEXT,

    service_termination_epoch BIGINT,

    delete_data_set_task_id BIGINT NOT NULL,
    after_delete_data_set INTEGER NOT NULL DEFAULT FALSE,
    delete_tx_hash TEXT,

    terminated INTEGER NOT NULL DEFAULT FALSE
);

-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1
--         FROM information_schema.columns
--         WHERE table_name = 'pdp_data_set_pieces'
--           AND table_schema = current_schema()
--           AND column_name = 'rm_message_hash'
--     ) THEN
--         ALTER TABLE pdp_data_set_pieces ADD COLUMN IF NOT EXISTS rm_message_hash TEXT DEFAULT NULL;
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
--         WHERE table_name = 'pdp_data_set_pieces'
--           AND table_schema = current_schema()
--           AND column_name = 'removed'
--     ) THEN
--         ALTER TABLE pdp_data_set_pieces ADD COLUMN IF NOT EXISTS removed INTEGER DEFAULT FALSE;
--     END IF;
-- END
-- $$;


CREATE INDEX IF NOT EXISTS pdp_piecerefs_piece_cid_idx ON pdp_piecerefs (piece_cid);
