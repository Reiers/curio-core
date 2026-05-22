-- Mk20 deal protocol tables (the subset PDP touches).
--
-- PDP writes to market_mk20_deal.pdp_v1 (a JSON column) and reads from
-- market_piece_deal for piece-location lookups. We carry only what PDP
-- actually references, not the full mk20 sealing pipeline (market_mk20_pipeline,
-- which is for PoRep deals not PDP).
--
-- Source files folded in:
--   20240731-market-migration.sql   (market_piece_deal base)
--   20250505-market-mk20.sql        (market_mk20_deal, schema reshape on
--                                    market_piece_deal: drop old PK,
--                                    new PK (id, sp_id, piece_cid, piece_length),
--                                    add piece_ref column)
--   20260125-fix-process_piece_deal.sql  (the PG function that maintains the
--                                          summary; reimplemented in Go.)
--   20260416-mk20-ddo-contracts.sql       (ddo_contracts allowlist)

CREATE TABLE IF NOT EXISTS market_mk20_deal (
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    id          TEXT PRIMARY KEY,                       -- ULID
    client      TEXT NOT NULL,

    piece_cid_v2 TEXT,

    -- JSONB columns -> TEXT. The Go layer json-encodes / json_extract()'s
    -- via SQLite's JSON1 extension (built into modernc.org/sqlite).
    data         TEXT NOT NULL DEFAULT 'null',
    ddo_v1       TEXT NOT NULL DEFAULT 'null',
    retrieval_v1 TEXT NOT NULL DEFAULT 'null',
    pdp_v1       TEXT NOT NULL DEFAULT 'null'
);

-- market_piece_deal: piece location index. Post-2025-05-05 schema (the
-- old (sp_id, piece_cid, id) primary key got swapped to
-- (id, sp_id, piece_cid, piece_length) and piece_offset was made nullable).
CREATE TABLE IF NOT EXISTS market_piece_deal (
    id           TEXT NOT NULL,                          -- UUID for new, PropCID for old
    boost_deal   INTEGER NOT NULL,
    legacy_deal  INTEGER NOT NULL DEFAULT 0,
    chain_deal_id INTEGER NOT NULL DEFAULT 0,
    sp_id        INTEGER NOT NULL,
    sector_num   INTEGER NOT NULL,
    piece_offset INTEGER,                                -- nullable post-2025-05-05
    piece_ref    INTEGER,                                -- added 20250505
    piece_cid    TEXT NOT NULL,
    piece_length INTEGER NOT NULL,
    raw_size     INTEGER NOT NULL,

    PRIMARY KEY (id, sp_id, piece_cid, piece_length)
);

CREATE UNIQUE INDEX IF NOT EXISTS market_piece_deal_identity_key
    ON market_piece_deal (sp_id, id);

-- 20260416-mk20-ddo-contracts.sql
CREATE TABLE IF NOT EXISTS ddo_contracts (
    address TEXT PRIMARY KEY,
    allowed INTEGER NOT NULL DEFAULT 0
);

-- 20251231 + 20260211: market_fix_raw_size (post-rekey form: id TEXT PRIMARY KEY)
CREATE TABLE IF NOT EXISTS market_fix_raw_size (
    id      TEXT PRIMARY KEY,
    task_id INTEGER NOT NULL
);
