-- Piece park: the disk-backed staging area for pieces that PDP (and the
-- mk20 deal pipeline) hand to the storage layer.
--
-- Hand-translated from upstream Curio's 20240228-piece-park.sql with the
-- PDP-era extensions from 20240930-pdp.sql folded in:
--   - parked_pieces.long_term (BOOLEAN -> INTEGER)
--   - parked_pieces unique constraint widened from (piece_cid) to
--     (piece_cid, piece_padded_size, long_term, cleanup_task_id).
--   - parked_piece_refs.long_term (BOOLEAN -> INTEGER)
--   - parked_pieces.skip from 20250505-market-mk20.sql

CREATE TABLE IF NOT EXISTS parked_pieces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    piece_cid TEXT NOT NULL,                       -- piece cid v1
    piece_padded_size INTEGER NOT NULL,
    piece_raw_size INTEGER NOT NULL,

    complete INTEGER NOT NULL DEFAULT 0,
    task_id INTEGER REFERENCES harmony_task (id) ON DELETE SET NULL,

    cleanup_task_id INTEGER REFERENCES harmony_task (id) ON DELETE SET NULL,

    long_term INTEGER NOT NULL DEFAULT 0,
    skip INTEGER NOT NULL DEFAULT 0,

    UNIQUE (piece_cid, piece_padded_size, long_term, cleanup_task_id)
);

CREATE TABLE IF NOT EXISTS parked_piece_refs (
    ref_id INTEGER PRIMARY KEY AUTOINCREMENT,
    piece_id INTEGER NOT NULL REFERENCES parked_pieces (id) ON DELETE CASCADE,

    data_url TEXT,
    data_headers TEXT NOT NULL DEFAULT '{}',       -- jsonb -> TEXT (json-encoded)

    long_term INTEGER NOT NULL DEFAULT 0
);

-- Indexes from 20251014-park-piece-optimisation.sql:
CREATE INDEX IF NOT EXISTS idx_parked_piece_refs_piece_id
    ON parked_piece_refs (piece_id);

CREATE INDEX IF NOT EXISTS idx_parked_pieces_cleanup_null
    ON parked_pieces (id) WHERE cleanup_task_id IS NULL;
