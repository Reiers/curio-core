-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20240929-chain-sends-eth.sql
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
-- TODO (manual): the source file contained PG-specific constructs that
-- can't be auto-translated 1:1. Search for `-- TODO: PG-` markers below.
-- Flagged constructs: COMMENT ON, JSONB (mapped to TEXT)
--

CREATE TABLE IF NOT EXISTS eth_keys (
    address TEXT NOT NULL PRIMARY KEY,
    private_key BLOB NOT NULL,
    role TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS message_sends_eth
(
    from_address  TEXT   NOT NULL,
    to_address    TEXT   NOT NULL,
    send_reason   TEXT   NOT NULL,
    send_task_id  INTEGER PRIMARY KEY AUTOINCREMENT,

    unsigned_tx   BLOB  NOT NULL,
    unsigned_hash TEXT   NOT NULL,

    nonce         BIGINT,
    signed_tx     BLOB,
    signed_hash   TEXT,

    send_time     DATETIME DEFAULT NULL,
    send_success  INTEGER   DEFAULT NULL,
    send_error    TEXT
);

-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.from_address IS 'Ethereum 0x... address';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.to_address IS 'Ethereum 0x... address';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.send_reason IS 'Optional description of send reason';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.send_task_id IS 'Task ID of the send task';

-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.unsigned_tx IS 'Unsigned transaction data';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.unsigned_hash IS 'Hash of the unsigned transaction';

-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.nonce IS 'Assigned transaction nonce, set while the send task is executing';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.signed_tx IS 'Signed transaction data, set while the send task is executing';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.signed_hash IS 'Hash of the signed transaction';

-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.send_time IS 'Time when the send task was executed, set after pushing the transaction to the network';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.send_success IS 'Whether this transaction was broadcasted to the network already, NULL if not yet attempted, TRUE if successful, FALSE if failed';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN message_sends_eth.send_error IS 'Error message if send_success is FALSE';

CREATE UNIQUE INDEX IF NOT EXISTS message_sends_eth_success_index
    ON message_sends_eth (from_address, nonce)
    WHERE send_success IS NOT FALSE;

-- (PG COMMENT ON stripped) COMMENT ON INDEX message_sends_eth_success_index IS     'message_sends_eth_success_index enforces sender/nonce uniqueness, it is a conditional index that only indexes rows where send_success is not false. This allows us to have multiple rows with the same sender/nonce, as long as only one of them was successfully broadcasted (true) to the network or is in the process of being broadcasted (null).';

CREATE TABLE IF NOT EXISTS message_send_eth_locks
(
    from_address TEXT      NOT NULL,
    task_id      BIGINT    NOT NULL,
    claimed_at   DATETIME NOT NULL,

    CONSTRAINT message_send_eth_locks_pk
        PRIMARY KEY (from_address)
);

CREATE TABLE IF NOT EXISTS message_waits_eth (
    signed_tx_hash TEXT PRIMARY KEY,
    waiter_machine_id INT REFERENCES harmony_machines (id) ON DELETE SET NULL,

    confirmed_block_number BIGINT,
    confirmed_tx_hash TEXT,
    confirmed_tx_data TEXT,

    tx_status TEXT, -- 'pending', 'confirmed', 'failed'
    tx_receipt TEXT,
    tx_success INTEGER
);

-- index for UPDATE message_waits_eth SET waiter_machine_id = $1 WHERE waiter_machine_id IS NULL AND tx_status = 'pending'
CREATE INDEX IF NOT EXISTS idx_message_waits_eth_pending
    ON message_waits_eth (waiter_machine_id)
    WHERE waiter_machine_id IS NULL AND tx_status = 'pending';
