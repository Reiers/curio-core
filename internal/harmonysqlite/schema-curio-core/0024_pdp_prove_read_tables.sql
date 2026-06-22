-- market_piece_metadata: the piece-size / indexed-state table that the
-- retrieval + prove read path (lib/cachedreader getPieceReader) consults to
-- resolve a v1 piece CID's padded size.
--
-- WHY THIS WAS MISSING / WHY IT MATTERS
-- 0013_market_mk20.sql ported the market subset PDP "writes" (market_mk20_deal,
-- market_piece_deal, ddo_contracts, market_fix_raw_size) but NOT
-- market_piece_metadata, because in curio-core the PDP add-piece path serves
-- pieces straight from the piece park and never runs the full-Curio
-- process_piece_deal() indexing function that populates this table.
--
-- The problem: cachedreader.getPieceReader resolves piece size with a single
-- COALESCE statement:
--     COALESCE(
--       (SELECT piece_size        FROM market_piece_metadata WHERE piece_cid=?),
--       (SELECT piece_padded_size FROM parked_pieces          WHERE piece_cid=?),
--       0)
-- For a parked PDP piece the real size lives in parked_pieces (fallback #2).
-- But because market_piece_metadata did not EXIST, SQLite aborted the whole
-- statement with "no such table: market_piece_metadata" BEFORE the COALESCE
-- could fall through to parked_pieces. That broke the very first mainnet PDP
-- proof on dataset 1311 (PDPv0_Prove task error, 2026-06-22): the prover
-- could not read the subpiece back to build the proof memtree.
--
-- THE FIX
-- Create the table (Postgres source: 20240731-market-migration.sql, PK reshaped
-- to (piece_cid, piece_size) in 20250505-market-mk20.sql). Empty is correct
-- for the park-served PDP flow: the COALESCE now falls through to parked_pieces
-- and the prove proceeds. When/if the indexing path runs, it has somewhere to
-- write. Types are SQLite-native (INTEGER for BIGINT/BOOLEAN, TEXT timestamps),
-- matching the conventions in 0013.
CREATE TABLE IF NOT EXISTS market_piece_metadata (
    piece_cid  TEXT NOT NULL,
    piece_size INTEGER NOT NULL,

    version    INTEGER NOT NULL DEFAULT 2,   -- Boost = v1; curio = v2

    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    indexed    INTEGER NOT NULL DEFAULT 0,   -- BOOLEAN
    indexed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),

    -- 20250505-market-mk20.sql reshaped the PK from (piece_cid) to
    -- (piece_cid, piece_size).
    PRIMARY KEY (piece_cid, piece_size)
);

-- sectors_meta: the SECOND table the same getPieceReader query needs.
-- After the piece-size COALESCE above, cachedreader.go (and
-- piecesunseal.go) run:
--     ... LEFT JOIN sectors_meta sm ON sm.sp_id = mpd.sp_id
--                                   AND sm.sector_num = mpd.sector_num
--     COALESCE(sm.reg_seal_proof, 0) AS reg_seal_proof
-- This was ALSO missing from the curio-core SQLite schema (we have
-- sector_location from the storage-index migration, not sectors_meta), so the
-- very next statement in the prove/retrieval path would have failed with
-- "no such table: sectors_meta" the moment the metadata lookup was unblocked.
--
-- For PDP the piece is park-served (not sector-served), so the LEFT JOIN finds
-- no row and reg_seal_proof correctly COALESCEs to 0. An empty table is the
-- right state for the PDP-only flow; the columns mirror the full-Curio
-- definition (20240425-sector_meta.sql) so a future PoRep/sector path can
-- populate it. SQLite-native types (INTEGER for BIGINT/INT, BLOB for BYTEA).
CREATE TABLE IF NOT EXISTS sectors_meta (
    sp_id             INTEGER NOT NULL,
    sector_num        INTEGER NOT NULL,

    reg_seal_proof    INTEGER NOT NULL,
    ticket_epoch      INTEGER NOT NULL,
    ticket_value      BLOB    NOT NULL,

    orig_sealed_cid   TEXT NOT NULL,
    orig_unsealed_cid TEXT NOT NULL,
    cur_sealed_cid    TEXT NOT NULL,
    cur_unsealed_cid  TEXT NOT NULL,

    msg_cid_precommit TEXT,
    msg_cid_commit    TEXT,
    msg_cid_update    TEXT,

    seed_epoch        INTEGER NOT NULL,
    seed_value        BLOB    NOT NULL,

    PRIMARY KEY (sp_id, sector_num)
);
