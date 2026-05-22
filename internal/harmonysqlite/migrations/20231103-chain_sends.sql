-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20231103-chain_sends.sql
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

create table if not exists message_sends
(
    from_key     text   not null,
    to_addr      text   not null,
    send_reason  text   not null,
    send_task_id bigint not null,

    unsigned_data BLOB not null,
    unsigned_cid  text  not null,

    nonce        bigint,
    signed_data  BLOB,
    signed_json  jsonb,
    signed_cid   text,

    send_time    DATETIME default null,
    send_success INTEGER   default null,
    send_error   text,

    constraint message_sends_pk
        primary key (send_task_id, from_key)
);

-- (PG COMMENT ON stripped) comment on column message_sends.from_key is 'text f[1/3/4]... address';
-- (PG COMMENT ON stripped) comment on column message_sends.to_addr is 'text f[0/1/2/3/4]... address';
-- (PG COMMENT ON stripped) comment on column message_sends.send_reason is 'optional description of send reason';
-- (PG COMMENT ON stripped) comment on column message_sends.send_task_id is 'harmony task id of the send task';

-- (PG COMMENT ON stripped) comment on column message_sends.unsigned_data is 'unsigned message data';
-- (PG COMMENT ON stripped) comment on column message_sends.unsigned_cid is 'unsigned message cid';

-- (PG COMMENT ON stripped) comment on column message_sends.nonce is 'assigned message nonce, set while the send task is executing';
-- (PG COMMENT ON stripped) comment on column message_sends.signed_data is 'signed message data, set while the send task is executing';
-- (PG COMMENT ON stripped) comment on column message_sends.signed_cid is 'signed message cid, set while the send task is executing';

-- (PG COMMENT ON stripped) comment on column message_sends.send_time is 'time when the send task was executed, set after pushing the message to the network';
-- (PG COMMENT ON stripped) comment on column message_sends.send_success is 'whether this message was broadcasted to the network already, null if not yet attempted, true if successful, false if failed';
-- (PG COMMENT ON stripped) comment on column message_sends.send_error is 'error message if send_success is false';

create unique index if not exists message_sends_success_index
    on message_sends (from_key, nonce)
    where send_success is not false;

-- (PG COMMENT ON stripped) comment on index message_sends_success_index is 'message_sends_success_index enforces sender/nonce uniqueness, it is a conditional index that only indexes rows where send_success is not false. This allows us to have multiple rows with the same sender/nonce, as long as only one of them was successfully broadcasted (true) to the network or is in the process of being broadcasted (null).';

create table if not exists message_send_locks
(
    from_key text   not null,
    task_id  bigint not null,
    claimed_at DATETIME not null,

    constraint message_send_locks_pk
        primary key (from_key)
);
