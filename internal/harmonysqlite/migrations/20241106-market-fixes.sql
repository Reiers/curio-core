-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20241106-market-fixes.sql
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

CREATE UNIQUE INDEX IF NOT EXISTS ipni_peerid_sp_id_unique ON ipni_peerid (sp_id);

CREATE UNIQUE INDEX IF NOT EXISTS sectors_pipeline_events_task_history_id_uindex
    ON sectors_pipeline_events (task_history_id, sp_id, sector_number);

CREATE UNIQUE INDEX IF NOT EXISTS market_piece_deal_piece_cid_id_uindex
    ON market_piece_deal (piece_cid, id);

ALTER TABLE market_mk12_deals
    ADD COLUMN IF NOT EXISTS proposal_cid text not null;

CREATE INDEX IF NOT EXISTS market_mk12_deals_proposal_cid_index
    ON market_mk12_deals (proposal_cid);


-- Add the is_skip column to the ipni table
ALTER TABLE ipni ADD COLUMN IF NOT EXISTS is_skip INTEGER NOT NULL DEFAULT FALSE; -- set to true means return 404 for related entries

DROP INDEX IF EXISTS ipni_context_id;
CREATE INDEX IF NOT EXISTS ipni_context_id ON ipni(context_id, ad_cid, is_rm, is_skip);

CREATE INDEX IF NOT EXISTS ipni_entries_skip ON ipni(entries, is_skip, piece_cid);