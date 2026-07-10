-- 0025_message_waits_eth_gc_prep.sql
--
-- The PDPv0 dataset lifecycle tables declared their eth-tx hash columns
-- as FKs into message_waits_eth with ON DELETE CASCADE:
--
--   pdp_data_set_creates.create_message_hash    -> message_waits_eth(signed_tx_hash) CASCADE
--   pdp_data_set_pieces.add_message_hash        -> message_waits_eth(signed_tx_hash) CASCADE
--   pdp_data_set_piece_adds.add_message_hash    -> message_waits_eth(signed_tx_hash) CASCADE
--
-- message_waits_eth is a confirmation-record table: rows are inserted
-- by the SendTaskETH lane, updated as the receipt confirms on-chain,
-- and are never a source of truth for PDPv0 state. With foreign_keys=ON
-- the CASCADE would erase dataset creates, piece rows, and piece adds
-- whenever a confirmed receipt row is pruned -- which is unacceptable:
-- the PDPv0 tables are the authoritative record of who owns what.
--
-- Rebuild the three tables with the message_waits_eth FK dropped so the
-- new eth-message-wait GC can retire old confirmed rows without touching
-- PDPv0 state. All other FKs and indexes are preserved verbatim from
-- 0011.
--
-- Triggers that fire on / write into these tables must be dropped
-- first (SQLite refuses to DROP TABLE while a trigger references it as
-- the update target) and re-created after the rename. Both the pieces-
-- refcount triggers on pdp_data_set_pieces and the message-status
-- propagators on message_waits_eth are restored verbatim from 0011.
--
-- Idempotency: uses IF NOT EXISTS + INSERT OR IGNORE, and re-runs are
-- no-ops after the first successful application because the rebuilt
-- tables no longer declare the message_waits_eth FK.

-- ---------------------------------------------------------------------
-- 1. Drop triggers that reference the tables being rebuilt.
-- ---------------------------------------------------------------------

DROP TRIGGER IF EXISTS pdp_data_set_piece_insert;
DROP TRIGGER IF EXISTS pdp_data_set_piece_delete;
DROP TRIGGER IF EXISTS pdp_data_set_create_message_status_change;
DROP TRIGGER IF EXISTS pdp_data_set_add_message_status_change;

-- ---------------------------------------------------------------------
-- 2. Rebuild pdp_data_set_creates without the message_waits_eth FK.
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS pdp_data_set_creates__new (
    create_message_hash TEXT PRIMARY KEY,
    ok                  INTEGER,
    data_set_created    INTEGER NOT NULL DEFAULT 0,
    service             TEXT NOT NULL
                          REFERENCES pdp_services(service_label) ON DELETE CASCADE,
    created_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO pdp_data_set_creates__new
    (create_message_hash, ok, data_set_created, service, created_at)
SELECT create_message_hash, ok, data_set_created, service, created_at
FROM pdp_data_set_creates;

DROP TABLE pdp_data_set_creates;
ALTER TABLE pdp_data_set_creates__new RENAME TO pdp_data_set_creates;

-- ---------------------------------------------------------------------
-- 3. Rebuild pdp_data_set_pieces without the message_waits_eth FK.
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS pdp_data_set_pieces__new (
    data_set          INTEGER NOT NULL REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    piece             TEXT NOT NULL,
    add_message_hash  TEXT NOT NULL,
    add_message_index INTEGER NOT NULL,
    piece_id          INTEGER NOT NULL,
    sub_piece         TEXT NOT NULL,
    sub_piece_offset  INTEGER NOT NULL,
    sub_piece_size    INTEGER NOT NULL,
    pdp_pieceref      INTEGER REFERENCES pdp_piecerefs(id) ON DELETE CASCADE,
    PRIMARY KEY (data_set, piece_id, sub_piece_offset)
);

INSERT OR IGNORE INTO pdp_data_set_pieces__new
    (data_set, piece, add_message_hash, add_message_index, piece_id,
     sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
SELECT data_set, piece, add_message_hash, add_message_index, piece_id,
       sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref
FROM pdp_data_set_pieces;

DROP TABLE pdp_data_set_pieces;
ALTER TABLE pdp_data_set_pieces__new RENAME TO pdp_data_set_pieces;

-- ---------------------------------------------------------------------
-- 4. Rebuild pdp_data_set_piece_adds without the message_waits_eth FK.
-- ---------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS pdp_data_set_piece_adds__new (
    data_set          INTEGER REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    piece             TEXT NOT NULL,
    add_message_hash  TEXT NOT NULL,
    add_message_ok    INTEGER,
    add_message_index INTEGER NOT NULL,
    sub_piece         TEXT NOT NULL,
    sub_piece_offset  INTEGER NOT NULL,
    sub_piece_size    INTEGER NOT NULL,
    pdp_pieceref      INTEGER REFERENCES pdp_piecerefs(id) ON DELETE CASCADE,
    pieces_added      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (add_message_hash, sub_piece_offset)
);

INSERT OR IGNORE INTO pdp_data_set_piece_adds__new
    (data_set, piece, add_message_hash, add_message_ok, add_message_index,
     sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref, pieces_added)
SELECT data_set, piece, add_message_hash, add_message_ok, add_message_index,
       sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref, pieces_added
FROM pdp_data_set_piece_adds;

DROP TABLE pdp_data_set_piece_adds;
ALTER TABLE pdp_data_set_piece_adds__new RENAME TO pdp_data_set_piece_adds;

-- ---------------------------------------------------------------------
-- 5. Restore the indexes and triggers (verbatim from 0011).
-- ---------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS pdp_data_set_piece_adds_pdp_pieceref_idx
    ON pdp_data_set_piece_adds(pdp_pieceref)
    WHERE pdp_pieceref IS NOT NULL;

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

-- ---------------------------------------------------------------------
-- 6. Speed up the GC's confirmed-and-old scan.
-- ---------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_message_waits_eth_confirmed_block
    ON message_waits_eth (confirmed_block_number)
    WHERE tx_status = 'confirmed' AND confirmed_block_number IS NOT NULL;
