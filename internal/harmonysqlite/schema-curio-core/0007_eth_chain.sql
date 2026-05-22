-- Eth chain plumbing: keys + sends + waits.
-- Hand-translated from upstream Curio's 20240929-chain-sends-eth.sql.
--
-- PDP signs and broadcasts Ethereum transactions for proofset creation,
-- root adds, and proving-period init. The send/wait queue here is the
-- eth-side analogue of the FIL-side message_sends + message_waits in
-- 0004 / 0005.
--
-- Translation notes:
--   SERIAL                    -> INTEGER PRIMARY KEY AUTOINCREMENT
--   TIMESTAMP                 -> TEXT (ISO-8601 strings from the Go layer)
--   BYTEA                     -> BLOB
--   JSONB                     -> TEXT (json-encoded at the Go layer)
--   BOOLEAN                   -> INTEGER (0/1)
--   COMMENT ON                -> dropped (SQLite has no column comments)

CREATE TABLE IF NOT EXISTS eth_keys (
    address TEXT NOT NULL PRIMARY KEY,
    private_key BLOB NOT NULL,
    role TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS message_sends_eth (
    from_address  TEXT NOT NULL,
    to_address    TEXT NOT NULL,
    send_reason   TEXT NOT NULL,
    send_task_id  INTEGER PRIMARY KEY AUTOINCREMENT,

    unsigned_tx   BLOB NOT NULL,
    unsigned_hash TEXT NOT NULL,

    nonce         INTEGER,
    signed_tx     BLOB,
    signed_hash   TEXT,

    send_time     TEXT,
    send_success  INTEGER,
    send_error    TEXT
);

-- Partial unique index: only enforce sender+nonce uniqueness for rows
-- that haven't already failed. Matches the PG `WHERE send_success IS NOT FALSE`
-- exactly; SQLite supports partial indexes.
CREATE UNIQUE INDEX IF NOT EXISTS message_sends_eth_success_index
    ON message_sends_eth (from_address, nonce)
    WHERE send_success IS NOT 0;

CREATE TABLE IF NOT EXISTS message_send_eth_locks (
    from_address TEXT NOT NULL PRIMARY KEY,
    task_id      INTEGER NOT NULL,
    claimed_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS message_waits_eth (
    signed_tx_hash TEXT PRIMARY KEY,
    waiter_machine_id INTEGER REFERENCES harmony_machines (id) ON DELETE SET NULL,

    confirmed_block_number INTEGER,
    confirmed_tx_hash TEXT,
    confirmed_tx_data TEXT,    -- jsonb -> TEXT

    tx_status TEXT,            -- 'pending' | 'confirmed' | 'failed'
    tx_receipt TEXT,           -- jsonb -> TEXT
    tx_success INTEGER
);

CREATE INDEX IF NOT EXISTS idx_message_waits_eth_pending
    ON message_waits_eth (waiter_machine_id)
    WHERE waiter_machine_id IS NULL AND tx_status = 'pending';
