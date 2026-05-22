-- IPNI advertising state.
-- Hand-translated from upstream Curio's 20240823-ipni.sql with
-- 20241106-market-fixes.sql + 20260410-ipni-head-cas.sql changes folded in.
--
-- pdpv0/watch_piece_delete.go and pdpv0/ipni_announce.go both read from
-- these tables. We keep the schema thin: no ipni_task (sealing-side),
-- but we DO keep ipni, ipni_head, ipni_peerid, ipni_chunks, ipni_ad_fetches.
--
-- The upstream CAS-style insert function (insert_ad_and_update_head_checked)
-- is reimplemented at the Go layer (see pkg/ipni for the planned home);
-- this file is schema-only.

CREATE TABLE IF NOT EXISTS ipni_peerid (
    priv_key BLOB NOT NULL PRIMARY KEY,
    peer_id  TEXT NOT NULL UNIQUE,
    sp_id    INTEGER NOT NULL UNIQUE      -- 20241106-market-fixes.sql added UNIQUE
);

CREATE TABLE IF NOT EXISTS ipni (
    order_number INTEGER PRIMARY KEY AUTOINCREMENT,
    ad_cid       TEXT NOT NULL UNIQUE,
    context_id   BLOB NOT NULL,
    metadata     BLOB NOT NULL DEFAULT x'a01200',     -- 20250505-market-mk20.sql
    is_rm        INTEGER NOT NULL,
    is_skip      INTEGER NOT NULL DEFAULT 0,           -- 20241106-market-fixes.sql

    previous     TEXT,

    provider     TEXT NOT NULL,
    addresses    TEXT NOT NULL,

    signature    BLOB NOT NULL,
    entries      TEXT NOT NULL,

    piece_cid    TEXT NOT NULL,
    piece_size   INTEGER NOT NULL,
    piece_cid_v2 TEXT                                  -- 20250505-market-mk20.sql
);

CREATE INDEX IF NOT EXISTS ipni_provider_order_number ON ipni(provider, order_number);
CREATE UNIQUE INDEX IF NOT EXISTS ipni_ad_cid ON ipni(ad_cid);
-- The post-20241106 form is non-unique to allow multiple is_skip=1 rows
-- for the same context_id (deletions over time).
CREATE INDEX IF NOT EXISTS ipni_context_id ON ipni(context_id, ad_cid, is_rm, is_skip);
CREATE INDEX IF NOT EXISTS ipni_entries_skip ON ipni(entries, is_skip, piece_cid);
CREATE INDEX IF NOT EXISTS ipni_provider_ad_cid ON ipni(provider, ad_cid);

CREATE TABLE IF NOT EXISTS ipni_head (
    provider TEXT NOT NULL PRIMARY KEY,
    head     TEXT NOT NULL,
    FOREIGN KEY (head) REFERENCES ipni(ad_cid) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS ipni_chunks (
    cid       TEXT PRIMARY KEY,
    piece_cid TEXT NOT NULL,
    chunk_num INTEGER NOT NULL,
    first_cid TEXT,
    start_offset INTEGER,
    num_blocks INTEGER NOT NULL,
    from_car INTEGER NOT NULL,
    CHECK (
        (from_car = 0 AND first_cid IS NOT NULL AND start_offset IS NULL) OR
        (from_car = 1 AND first_cid IS NULL AND start_offset IS NOT NULL)
    ),
    UNIQUE (piece_cid, chunk_num)
);

-- 20251011-pdp-v0-ipni-fetch-tracking.sql
CREATE TABLE IF NOT EXISTS ipni_ad_fetches (
    ad_cid       TEXT NOT NULL,
    fetched_by   TEXT NOT NULL,
    fetched_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    fetch_method TEXT,                                 -- http | graphsync | ...
    PRIMARY KEY (ad_cid, fetched_by, fetched_at)
);
