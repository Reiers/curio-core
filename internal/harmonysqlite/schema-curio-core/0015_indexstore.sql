-- Indexstore tables for the SQLite-backed indexstore.Backend implementation
-- (internal/sqliteindex). Curio Core's hot-storage SP deployment shape
-- doesn't run Cassandra; the upstream `market/indexstore` package depends
-- on gocql against a Cassandra/ScyllaDB cluster. Our SQLite-backed
-- implementation satisfies the same interface against this schema.
--
-- Translation source: market/indexstore/cql/0001_create.cql +
-- 0002_piece_index.cql. Only the tables curio-core's pdpv0 + cachedreader
-- code path actively touches are ported.
--
-- pdpv0 active surface (from `grep idx.* tasks/pdpv0/`):
--   AddPDPLayer       INSERT pdp_cache_layer
--   GetPDPLayer       SELECT pdp_cache_layer
--   GetPDPLayerIndex  SELECT MAX layer index
--   GetPDPNode        SELECT single leaf
--
-- cachedreader active surface:
--   FindPieceInAggregate  SELECT piece_by_aggregate (mk20-only; pdpv0
--                          returns empty cleanly, so this table exists
--                          for interface completeness but is never
--                          populated in pdpv0-only deployments)

-- pdp_cache_layer is the load-bearing table for ProveTask. Holds the
-- precomputed Merkle tree leaves for every piece in every layer. On a
-- challenge, ProveTask reads one leaf at the challenged index per layer
-- to construct the proof.
--
-- Cassandra: PRIMARY KEY ((PieceCid, LayerIndex), LeafIndex) with
-- clustering order on LeafIndex ASC. SQLite equivalent: composite PK
-- with the same logical ordering; SQLite's PK is sorted by definition.
CREATE TABLE IF NOT EXISTS pdp_cache_layer (
    piece_cid   BLOB NOT NULL,
    layer_index INTEGER NOT NULL,
    leaf_index  INTEGER NOT NULL,
    leaf        BLOB NOT NULL,
    PRIMARY KEY (piece_cid, layer_index, leaf_index)
);

-- Index for the GetPDPLayerIndex lookup (find highest layer for a piece).
-- The PK covers (piece_cid, layer_index, ...) so SQLite can use the PK
-- index for both prefix lookups; this explicit index is belt-and-braces.
CREATE INDEX IF NOT EXISTS idx_pdp_cache_layer_piece_layer
    ON pdp_cache_layer (piece_cid, layer_index);

-- piece_by_aggregate is the mk20 aggregate-piece mapping. Curio Core is
-- pdpv0-only; this table is included for interface completeness (so the
-- FindPieceInAggregate query succeeds with empty results) but is never
-- written to in our deployment shape.
CREATE TABLE IF NOT EXISTS piece_by_aggregate (
    aggregate_piece_cid BLOB NOT NULL,
    piece_cid           BLOB NOT NULL,
    unpadded_offset     INTEGER NOT NULL,
    unpadded_length     INTEGER NOT NULL,
    PRIMARY KEY (aggregate_piece_cid, piece_cid, unpadded_offset)
);
