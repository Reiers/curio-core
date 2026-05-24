-- PDP v0 schema (the post-rename terminology used by the current PDPVerifier
-- contract: proofset->data_set, root->piece, subroot->sub_piece).
--
-- This file replaces the (upstream) sequence:
--   20250730-pdp-v0-rename.sql  (renames + new triggers)
--   20250930-pdp-v0-streaming-upload.sql
--   20251004-pdp-v0-indexing.sql
--   20251010 / 20260414 -pdp-v0-fix-add-piece-constraints.sql
--   20251015-pdp-v0-piece-adds-datasetid-nullable.sql
--   20251027-pdp-v0-filecoin-pay.sql
--   20251029-pdp-v0-pieceref-cascade.sql
--   20260109-pdp-v0-pull.sql
--   20260110-pdp-v0-termination-handling.sql
--   20260112-pdp-v0-efficiency-indexes.sql
--   20260122-pdp-v0-deletion-allowed.sql
--   20260123-pdp-v0-rename-terminated-at-epoch.sql
--   20260203-pdp-v0-delete-task-nullable.sql
--   20260216-pdp-v0-save-cache.sql
--   20260511-pdpv0-ipni-tracking.sql
--
-- Curio Core is greenfield: we define the post-rename schema directly,
-- skipping the rename migrations entirely. The v1 tables in 0010_pdp_v1.sql
-- are kept for compatibility with services that still use the v1
-- vocabulary (notify_url path, mh_to_commp lookups).

-- pdp_data_sets (formerly pdp_proof_sets).
CREATE TABLE IF NOT EXISTS pdp_data_sets (
    id INTEGER PRIMARY KEY,                            -- on-chain data set id

    prev_challenge_request_epoch INTEGER,
    challenge_request_task_id    INTEGER REFERENCES harmony_task(id) ON DELETE SET NULL,
    challenge_request_msg_hash   TEXT,

    proving_period   INTEGER,
    challenge_window INTEGER,
    prove_at_epoch   INTEGER,
    init_ready       INTEGER NOT NULL DEFAULT 0,

    create_message_hash TEXT NOT NULL,
    service             TEXT NOT NULL
                         REFERENCES pdp_services(service_label) ON DELETE RESTRICT,

    -- 20260110-pdp-v0-termination-handling.sql:
    terminated_at_epoch INTEGER,                       -- renamed below
    unrecoverable_proving_failure_epoch INTEGER,       -- 20260123 rename
    last_termination_attempt_at TEXT,
    termination_backoff_until   TEXT,
    consecutive_prove_failures INTEGER NOT NULL DEFAULT 0,
    next_prove_attempt_at      INTEGER,

    -- 20260122-pdp-v0-deletion-allowed.sql:
    deletion_allowed INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS pdp_data_set_creates (
    create_message_hash TEXT PRIMARY KEY
                          REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    ok                  INTEGER,
    data_set_created    INTEGER NOT NULL DEFAULT 0,    -- formerly proofset_created
    service             TEXT NOT NULL
                          REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Pieces in a data set (the renamed pdp_proofset_roots).
CREATE TABLE IF NOT EXISTS pdp_data_set_pieces (
    data_set  INTEGER NOT NULL REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    piece     TEXT NOT NULL,
    add_message_hash TEXT NOT NULL
                         REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_index INTEGER NOT NULL,
    piece_id  INTEGER NOT NULL,
    sub_piece TEXT NOT NULL,
    sub_piece_offset INTEGER NOT NULL,
    sub_piece_size   INTEGER NOT NULL,
    pdp_pieceref INTEGER REFERENCES pdp_piecerefs(id) ON DELETE CASCADE,  -- 20251029 cascade
    PRIMARY KEY (data_set, piece_id, sub_piece_offset)
);

CREATE TABLE IF NOT EXISTS pdp_data_set_piece_adds (
    data_set  INTEGER REFERENCES pdp_data_sets(id) ON DELETE CASCADE,    -- nullable per 20251015
    piece     TEXT NOT NULL,
    add_message_hash TEXT NOT NULL
                         REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_ok INTEGER,
    add_message_index INTEGER NOT NULL,
    sub_piece    TEXT NOT NULL,
    sub_piece_offset INTEGER NOT NULL,
    sub_piece_size   INTEGER NOT NULL,
    pdp_pieceref INTEGER REFERENCES pdp_piecerefs(id) ON DELETE CASCADE, -- 20251029 cascade
    pieces_added INTEGER NOT NULL DEFAULT 0,                              -- formerly roots_added
    PRIMARY KEY (add_message_hash, sub_piece_offset)
);

-- Mirrors filecoin-project/curio#1248 / PR#1198:
-- The chain-notification consumer joins pdp_data_set_piece_adds against
-- pdp_piecerefs by pdp_pieceref. Without this index that join is a
-- full scan per-tipset, paralysing the consumer under load. Partial
-- index on the non-null case keeps it small.
CREATE INDEX IF NOT EXISTS pdp_data_set_piece_adds_pdp_pieceref_idx
    ON pdp_data_set_piece_adds(pdp_pieceref)
    WHERE pdp_pieceref IS NOT NULL;

-- 20250930-pdp-v0-streaming-upload.sql
-- Schema parity with upstream Curio's PostgreSQL version. The
-- streaming-upload handler writes piece_size, raw_size, complete,
-- completed_at during finalize; earlier curio-core port was missing
-- those columns and the UPDATE returned 'no such column: piece_size'.
CREATE TABLE IF NOT EXISTS pdp_piece_streaming_uploads (
    id           TEXT PRIMARY KEY NOT NULL,       -- UUID
    service      TEXT NOT NULL,                    -- pdp_services.id (or "public" under NullAuth)
    piece_cid    TEXT,                             -- piece cid v1
    piece_size   INTEGER,                          -- BIGINT in upstream; padded piece size
    raw_size     INTEGER,                          -- BIGINT in upstream; original unpadded size
    piece_ref    INTEGER,                          -- parked_piece_refs.ref_id
    notify_url   TEXT,
    expires_at   TEXT,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    complete     INTEGER,                          -- bool in upstream; 0/1 here
    completed_at TEXT                              -- TIMESTAMPTZ in upstream
);

-- 20251027-pdp-v0-filecoin-pay.sql: filecoin_payment_transactions is
-- defined below near the other dialect-shimmed tables (search for
-- 'filecoin_payment_transactions' lower in this file). Block elided
-- here to avoid duplicate definitions; both used IF NOT EXISTS so the
-- duplication was harmless at runtime but confusing on inspection.

-- 20260109-pdp-v0-pull.sql
CREATE TABLE IF NOT EXISTS pdp_piece_pulls (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    source_url         TEXT NOT NULL,
    expected_piece_cid TEXT,
    expected_piece_size INTEGER,
    state              TEXT NOT NULL DEFAULT 'pending',
    created_at         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at       TEXT,
    error              TEXT
);

CREATE TABLE IF NOT EXISTS pdp_piece_pull_items (
    pull_id   INTEGER NOT NULL REFERENCES pdp_piece_pulls(id) ON DELETE CASCADE,
    piece_cid TEXT NOT NULL,
    piece_ref INTEGER REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL,
    PRIMARY KEY (pull_id, piece_cid)
);

----------------------------------------------------------------------------
-- Maintenance triggers for pdp_piecerefs.data_set_refcount.
-- Reimplements the PG functions from the rename migration as inline SQLite
-- triggers (no plpgsql).
----------------------------------------------------------------------------

CREATE TRIGGER IF NOT EXISTS pdp_data_set_piece_insert
AFTER INSERT ON pdp_data_set_pieces
WHEN NEW.pdp_pieceref IS NOT NULL
BEGIN
    UPDATE pdp_piecerefs
       SET data_set_refcount = data_set_refcount + 1
     WHERE id = NEW.pdp_pieceref;
END;

CREATE TRIGGER IF NOT EXISTS pdp_data_set_piece_delete
AFTER DELETE ON pdp_data_set_pieces
WHEN OLD.pdp_pieceref IS NOT NULL
BEGIN
    UPDATE pdp_piecerefs
       SET data_set_refcount = data_set_refcount - 1
     WHERE id = OLD.pdp_pieceref;
END;

-- Apply a column to pdp_piecerefs to carry the renamed refcount. The v1
-- migration defined proofset_refcount; we add data_set_refcount as a peer
-- column rather than rename, because SQLite ALTER ... RENAME COLUMN exists
-- (3.25+) but the trigger above must not break on existing rows.
ALTER TABLE pdp_piecerefs ADD COLUMN data_set_refcount INTEGER NOT NULL DEFAULT 0;

-- Backfill (no-op on fresh DB, harmless on re-run).
UPDATE pdp_piecerefs SET data_set_refcount = proofset_refcount;

-- 20260216-pdp-v0-save-cache.sql: SaveCache task tracking columns on
-- pdp_piecerefs. needs_save_cache defaults to TRUE so newly-uploaded
-- pieces get processed by the SaveCache task (which builds the Merkle
-- layer cache that ProveTask reads on challenge).
ALTER TABLE pdp_piecerefs ADD COLUMN needs_save_cache INTEGER NOT NULL DEFAULT 1;
ALTER TABLE pdp_piecerefs ADD COLUMN save_cache_task_id INTEGER
    REFERENCES harmony_task(id) ON DELETE SET NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_started TEXT DEFAULT NULL;
ALTER TABLE pdp_piecerefs ADD COLUMN caching_task_completed TEXT DEFAULT NULL;

-- 20260112-pdp-v0-efficiency-indexes.sql (the load-bearing ones):
CREATE INDEX IF NOT EXISTS idx_pdp_piece_uploads_notify
    ON pdp_piece_uploads (piece_ref)
    WHERE piece_ref IS NOT NULL AND notify_task_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_pdp_piecerefs_data_set_refcount
    ON pdp_piecerefs (data_set_refcount);

-- 20251027-pdp-v0-filecoin-pay.sql: pdp_delete_data_set tracks the
-- two-phase termination flow for PDP data sets. SQLite port: BIGINT
-- becomes INTEGER (SQLite is rank-flexible), BOOLEAN stays a 0/1
-- INTEGER alias which CHECK constraint doesn't need (upstream takes
-- the same trade).
--
-- Schema source: harmony/harmonydb/sql/20251027-pdp-v0-filecoin-pay.sql
-- (table CREATE) plus 20260122-pdp-v0-deletion-allowed.sql (ADD COLUMN
-- deletion_allowed). We fold both into one CREATE here since SQLite
-- is greenfield.
CREATE TABLE IF NOT EXISTS pdp_delete_data_set (
    id INTEGER PRIMARY KEY,

    terminate_service_task_id INTEGER,
    after_terminate_service   INTEGER NOT NULL DEFAULT 0,  -- BOOLEAN
    terminate_tx_hash         TEXT,

    service_termination_epoch INTEGER,

    delete_data_set_task_id   INTEGER,                     -- nullable per 20260203
    after_delete_data_set     INTEGER NOT NULL DEFAULT 0,  -- BOOLEAN
    delete_tx_hash            TEXT,

    terminated                INTEGER NOT NULL DEFAULT 0,  -- BOOLEAN
    deletion_allowed          INTEGER NOT NULL DEFAULT 0   -- BOOLEAN
);

-- filecoin_payment_transactions: tx_hash -> rail_ids list. SQLite has
-- no BIGINT[]; we store rail_ids as a JSON-encoded array TEXT column.
-- Callers that read this on SQLite use json_each() for iteration.
-- Pure-Go callers in curio's pdpv0 code path don't read this column
-- today (it's written by the filecoin-pay sweep and read by the
-- WebUI), so the TEXT shape is invisible to the active code surface.
CREATE TABLE IF NOT EXISTS filecoin_payment_transactions (
    tx_hash  TEXT PRIMARY KEY,
    rail_ids TEXT NOT NULL DEFAULT '[]'                    -- JSON array of bigints
);

-- Maintenance trigger: propagate message_waits_eth pending → confirmed/failed
-- into pdp_data_set_creates.ok. Upstream PG function:
--   update_pdp_data_set_creates() in 20250730-pdp-v0-rename.sql.
-- Mirror of pdp_proofset_create_message_status_change in 0010 for the
-- v1 names; needed for the dataset_watch handler to advance rows past
-- the `ok IS NULL` guard in processPendingDataSetCreates.
CREATE TRIGGER IF NOT EXISTS pdp_data_set_create_message_status_change
AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
WHEN OLD.tx_status = 'pending'
   AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed')
BEGIN
    UPDATE pdp_data_set_creates
       SET ok = CASE
                  WHEN NEW.tx_status = 'failed' OR NEW.tx_success = 0 THEN 0
                  WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = 1 THEN 1
                  ELSE ok
                END
     WHERE create_message_hash = NEW.signed_tx_hash
       AND data_set_created = 0;
END;

-- Same pattern for pdp_data_set_piece_adds.add_message_ok, used by the
-- addPieces flow. Upstream PG function: update_pdp_data_set_piece_adds()
-- in 20250730-pdp-v0-rename.sql.
CREATE TRIGGER IF NOT EXISTS pdp_data_set_add_message_status_change
AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
WHEN OLD.tx_status = 'pending'
   AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed')
BEGIN
    UPDATE pdp_data_set_piece_adds
       SET add_message_ok = CASE
                  WHEN NEW.tx_status = 'failed' OR NEW.tx_success = 0 THEN 0
                  WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = 1 THEN 1
                  ELSE add_message_ok
                END
     WHERE add_message_hash = NEW.signed_tx_hash;
END;
