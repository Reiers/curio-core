-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20231225-message-waits.sql
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
-- Flagged constructs: JSONB (mapped to TEXT)
--

create table if not exists message_waits (
    signed_message_cid text primary key,
    waiter_machine_id int references harmony_machines (id) on delete set null,

    executed_tsk_cid text,
    executed_tsk_epoch bigint,
    executed_msg_cid text,
    executed_msg_data jsonb,

    executed_rcpt_exitcode bigint,
    executed_rcpt_return BLOB,
    executed_rcpt_gas_used bigint

    -- created_at timestampz (added in 20250422-msg-wait-DATETIME.sql)
)
