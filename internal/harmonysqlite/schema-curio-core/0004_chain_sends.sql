CREATE TABLE IF NOT EXISTS message_sends (
    from_key TEXT NOT NULL,
    to_addr TEXT NOT NULL,
    send_reason TEXT NOT NULL,
    send_task_id INTEGER NOT NULL,
    unsigned_data BLOB NOT NULL,
    unsigned_cid TEXT NOT NULL,
    nonce INTEGER,
    signed_data BLOB,
    signed_json TEXT,
    signed_cid TEXT,
    send_time TEXT,
    send_success INTEGER,
    send_error TEXT,
    PRIMARY KEY (send_task_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS message_sends_success_unique_cid_idx
    ON message_sends (signed_cid) WHERE send_success = 1;
CREATE TABLE IF NOT EXISTS message_send_locks (
    from_key TEXT NOT NULL,
    task_id INTEGER NOT NULL,
    claimed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_key)
);
