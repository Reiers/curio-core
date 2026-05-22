-- PDP v1 schema.
-- Hand-translated from upstream Curio's 20240930-pdp.sql.
--
-- Key differences from Postgres:
--   1. The PL/pgSQL functions in the upstream file (increment/decrement/adjust
--      proofset_refcount + update_pdp_proofset_creates + update_pdp_proofset_roots)
--      are reimplemented as inline-body SQLite triggers, which can do BEGIN/END
--      blocks with plain SQL but cannot call user-defined functions.
--   2. JSONB -> TEXT; the Go layer json-encodes confirmed_tx_data and tx_receipt.
--   3. ON DELETE RESTRICT on pdp_proof_sets.service is kept; SQLite supports it.

CREATE TABLE IF NOT EXISTS pdp_services (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    pubkey       BLOB NOT NULL UNIQUE,
    service_label TEXT NOT NULL UNIQUE,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pdp_piece_uploads (
    id              TEXT PRIMARY KEY NOT NULL,         -- UUID -> TEXT
    service         TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE CASCADE,

    check_hash_codec TEXT NOT NULL,
    check_hash       BLOB NOT NULL,
    check_size       INTEGER NOT NULL,

    piece_cid    TEXT,                                 -- piece cid v2 (nullable until known)
    notify_url   TEXT NOT NULL,
    notify_task_id INTEGER,                            -- harmony_task.id

    piece_ref    INTEGER REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pdp_piecerefs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    service     TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    piece_cid   TEXT NOT NULL,
    piece_ref   INTEGER NOT NULL UNIQUE
                  REFERENCES parked_piece_refs(ref_id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    proofset_refcount INTEGER NOT NULL DEFAULT 0       -- maintained by triggers below
);

CREATE INDEX IF NOT EXISTS pdp_piecerefs_piece_cid_idx
    ON pdp_piecerefs(piece_cid);

CREATE TABLE IF NOT EXISTS pdp_piece_mh_to_commp (
    mhash BLOB PRIMARY KEY,
    size  INTEGER NOT NULL,
    commp TEXT NOT NULL
);

-- PDP proof sets we maintain.
CREATE TABLE IF NOT EXISTS pdp_proof_sets (
    id INTEGER PRIMARY KEY,                            -- on-chain proofset id (NOT autoincrement)

    prev_challenge_request_epoch INTEGER,
    challenge_request_task_id    INTEGER REFERENCES harmony_task(id) ON DELETE SET NULL,
    challenge_request_msg_hash   TEXT,

    proving_period   INTEGER,
    challenge_window INTEGER,
    prove_at_epoch   INTEGER,
    init_ready       INTEGER NOT NULL DEFAULT 0,

    create_message_hash TEXT NOT NULL,
    service             TEXT NOT NULL
                         REFERENCES pdp_services(service_label) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS pdp_prove_tasks (
    proofset INTEGER NOT NULL REFERENCES pdp_proof_sets(id) ON DELETE CASCADE,
    task_id  INTEGER NOT NULL REFERENCES harmony_task(id) ON DELETE CASCADE,
    PRIMARY KEY (proofset, task_id)
);

CREATE TABLE IF NOT EXISTS pdp_proofset_creates (
    create_message_hash TEXT PRIMARY KEY
                          REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    ok                  INTEGER,
    proofset_created    INTEGER NOT NULL DEFAULT 0,
    service             TEXT NOT NULL REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pdp_proofset_roots (
    proofset INTEGER NOT NULL REFERENCES pdp_proof_sets(id) ON DELETE CASCADE,
    root     TEXT NOT NULL,
    add_message_hash TEXT NOT NULL
                         REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_index INTEGER NOT NULL,
    root_id    INTEGER NOT NULL,
    subroot    TEXT NOT NULL,
    subroot_offset INTEGER NOT NULL,
    subroot_size   INTEGER NOT NULL,
    pdp_pieceref INTEGER REFERENCES pdp_piecerefs(id) ON DELETE SET NULL,
    PRIMARY KEY (proofset, root_id, subroot_offset)
);

CREATE TABLE IF NOT EXISTS pdp_proofset_root_adds (
    proofset INTEGER NOT NULL REFERENCES pdp_proof_sets(id) ON DELETE CASCADE,
    root     TEXT NOT NULL,
    add_message_hash TEXT NOT NULL
                         REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
    add_message_ok INTEGER,
    add_message_index INTEGER NOT NULL,
    subroot    TEXT NOT NULL,
    subroot_offset INTEGER NOT NULL,
    subroot_size   INTEGER NOT NULL,
    pdp_pieceref INTEGER REFERENCES pdp_piecerefs(id) ON DELETE SET NULL,
    -- 20250113-pdp-never-delete.sql adds roots_added; we fold it in here:
    roots_added INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (proofset, add_message_hash, subroot_offset)
);

CREATE INDEX IF NOT EXISTS idx_pdp_proofset_root_adds_roots_added
    ON pdp_proofset_root_adds(roots_added);

----------------------------------------------------------------------------
-- Triggers: maintain pdp_piecerefs.proofset_refcount.
--
-- Upstream uses PL/pgSQL functions; SQLite has triggers but no functions,
-- so we inline the bodies.
----------------------------------------------------------------------------

CREATE TRIGGER IF NOT EXISTS pdp_proofset_root_insert
AFTER INSERT ON pdp_proofset_roots
WHEN NEW.pdp_pieceref IS NOT NULL
BEGIN
    UPDATE pdp_piecerefs
       SET proofset_refcount = proofset_refcount + 1
     WHERE id = NEW.pdp_pieceref;
END;

CREATE TRIGGER IF NOT EXISTS pdp_proofset_root_delete
AFTER DELETE ON pdp_proofset_roots
WHEN OLD.pdp_pieceref IS NOT NULL
BEGIN
    UPDATE pdp_piecerefs
       SET proofset_refcount = proofset_refcount - 1
     WHERE id = OLD.pdp_pieceref;
END;

CREATE TRIGGER IF NOT EXISTS pdp_proofset_root_update_dec
AFTER UPDATE ON pdp_proofset_roots
WHEN OLD.pdp_pieceref IS NOT NULL
   AND (NEW.pdp_pieceref IS NULL OR NEW.pdp_pieceref != OLD.pdp_pieceref)
BEGIN
    UPDATE pdp_piecerefs
       SET proofset_refcount = proofset_refcount - 1
     WHERE id = OLD.pdp_pieceref;
END;

CREATE TRIGGER IF NOT EXISTS pdp_proofset_root_update_inc
AFTER UPDATE ON pdp_proofset_roots
WHEN NEW.pdp_pieceref IS NOT NULL
   AND (OLD.pdp_pieceref IS NULL OR NEW.pdp_pieceref != OLD.pdp_pieceref)
BEGIN
    UPDATE pdp_piecerefs
       SET proofset_refcount = proofset_refcount + 1
     WHERE id = NEW.pdp_pieceref;
END;

-- Maintenance trigger 1: when a message_waits_eth pending → confirmed/failed,
-- propagate to pdp_proofset_creates.ok. Upstream PG function:
--   update_pdp_proofset_creates() (in 20240930-pdp.sql).
CREATE TRIGGER IF NOT EXISTS pdp_proofset_create_message_status_change
AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
WHEN OLD.tx_status = 'pending'
   AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed')
BEGIN
    UPDATE pdp_proofset_creates
       SET ok = CASE
                  WHEN NEW.tx_status = 'failed' OR NEW.tx_success = 0 THEN 0
                  WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = 1 THEN 1
                  ELSE ok
                END
     WHERE create_message_hash = NEW.signed_tx_hash
       AND proofset_created = 0;
END;

-- Maintenance trigger 2: same idea for pdp_proofset_root_adds.add_message_ok.
-- Upstream PG function: update_pdp_proofset_roots() (in 20240930-pdp.sql).
CREATE TRIGGER IF NOT EXISTS pdp_proofset_add_message_status_change
AFTER UPDATE OF tx_status, tx_success ON message_waits_eth
WHEN OLD.tx_status = 'pending'
   AND (NEW.tx_status = 'confirmed' OR NEW.tx_status = 'failed')
BEGIN
    UPDATE pdp_proofset_root_adds
       SET add_message_ok = CASE
                  WHEN NEW.tx_status = 'failed' OR NEW.tx_success = 0 THEN 0
                  WHEN NEW.tx_status = 'confirmed' AND NEW.tx_success = 1 THEN 1
                  ELSE add_message_ok
                END
     WHERE add_message_hash = NEW.signed_tx_hash;
END;

-- Seed the well-known "public" PDP service used for NullAuth access.
-- This mirrors 20250603-pdp-public-service.sql. We use a sentinel zero-length
-- pubkey so the row satisfies the NOT NULL constraint; the upstream PG file
-- uses a similar pattern.
INSERT INTO pdp_services (service_label, pubkey)
    VALUES ('public', x'')
    ON CONFLICT(service_label) DO NOTHING;
