-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20250930-pdp-v0-streaming-upload.sql
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

CREATE TABLE IF NOT EXISTS pdp_piece_streaming_uploads (
    id TEXT PRIMARY KEY NOT NULL,
    service TEXT NOT NULL, -- pdp_services.id

    piece_cid TEXT, -- piece cid v1
    piece_size BIGINT,
    raw_size BIGINT,

    piece_ref BIGINT, -- packed_piece_refs.ref_id

    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    complete INTEGER,
    completed_at DATETIME
);
