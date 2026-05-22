-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20241104-piece-info.sql
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
-- Flagged constructs: DO $$ block, CREATE FUNCTION (plpgsql), CREATE TRIGGER
--

-- Piece summary table. This table will always have 1 row only and will be updated
-- by triggers
CREATE TABLE IF NOT EXISTS piece_summary (
    id INTEGER PRIMARY KEY DEFAULT TRUE, -- Single-row identifier, always set to TRUE
    total BIGINT NOT NULL DEFAULT 0,
    indexed BIGINT NOT NULL DEFAULT 0,
    announced BIGINT NOT NULL DEFAULT 0,
    last_updated DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Insert the initial row
INSERT INTO piece_summary (id) VALUES (TRUE) ON CONFLICT DO NOTHING;

-- Function to update piece_summary when a new entry is added to market_piece_metadata
-- TODO: PG-CREATE-FUNCTION (plpgsql). SQLite has no PL/pgSQL.
-- Translation strategy: rewrite the function body as an application-layer
-- transaction in Go, or as a sequence of triggers on the table(s) involved.
-- CREATE OR REPLACE FUNCTION update_piece_summary()
-- RETURNS TRIGGER AS $$
-- DECLARE
--     total_count BIGINT;
--     indexed_count BIGINT;
--     announced_count BIGINT;
-- BEGIN
--     -- Count total entries in market_piece_metadata
--     SELECT COUNT(*) INTO total_count FROM market_piece_metadata;
-- 
--     -- Count entries in market_piece_metadata where indexed is true
--     SELECT COUNT(*) INTO indexed_count FROM market_piece_metadata WHERE indexed = TRUE;
-- 
--     -- Count entries in market_piece_metadata that match entries in ipni on piece_cid and piece_size
--     SELECT COUNT(*) INTO announced_count
--     FROM market_piece_metadata mpm
--              JOIN ipni i ON mpm.piece_cid = i.piece_cid AND mpm.piece_size = i.piece_size;
-- 
--     -- Update piece_summary with the new counts and set last_updated to now
--     UPDATE piece_summary
--     SET
--         total = total_count,
--         indexed = indexed_count,
--         announced = announced_count,
--         last_updated = CURRENT_TIMESTAMP;
-- 
--     RETURN NEW;
-- END;
-- $$ LANGUAGE plpgsql;


-- Trigger to call update_piece_summary function on insert to market_piece_metadata
-- TODO: PG-DO-block (PostgreSQL procedural). Original kept verbatim.
-- Translation strategy: split the DO block into the imperative SQL
-- statements it would execute; SQLite has no procedural language.
-- DO $$
-- BEGIN
--     IF NOT EXISTS (
--         SELECT 1 FROM pg_trigger 
--         WHERE tgname = 'trigger_update_piece_summary'
--     ) THEN
--         -- TODO: PG-CREATE-TRIGGER (calls a PG function we couldn't port).
-- Translation strategy: rewrite as a SQLite trigger with inline SQL body,
-- or move the logic to the application layer.
-- CREATE TRIGGER trigger_update_piece_summary AFTER INSERT OR UPDATE ON market_piece_metadata
-- --     FOR EACH ROW
-- --     EXECUTE FUNCTION update_piece_summary();

--     END IF;
-- END $$;






