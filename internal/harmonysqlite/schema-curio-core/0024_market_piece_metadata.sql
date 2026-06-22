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
