-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20251011-pdp-v0-ipni-fetch-tracking.sql
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

-- Track IPNI advertisement fetches to provide indexing status visibility
-- This table logs when advertisements are fetched by indexers

CREATE TABLE IF NOT EXISTS ipni_ad_fetches (
    ad_cid TEXT NOT NULL,
    fetched_at DATETIME NOT NULL DEFAULT NOW()
);

-- Index for efficient lookup by ad_cid and time-based queries
CREATE INDEX IF NOT EXISTS ipni_ad_fetches_ad_cid_time ON ipni_ad_fetches(ad_cid, fetched_at DESC);
