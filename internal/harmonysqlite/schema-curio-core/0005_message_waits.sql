CREATE TABLE IF NOT EXISTS message_waits (
    signed_message_cid TEXT PRIMARY KEY,
    waiter_machine_id INTEGER REFERENCES harmony_machines(id) ON DELETE SET NULL,
    executed_tsk_cid TEXT,
    executed_tsk_epoch INTEGER,
    executed_msg_cid TEXT,
    executed_msg_data BLOB,
    executed_rcpt_exitcode INTEGER,
    executed_rcpt_return BLOB,
    executed_rcpt_gas_used INTEGER
);
