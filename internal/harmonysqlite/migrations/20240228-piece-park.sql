-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20240228-piece-park.sql
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

create table if not exists parked_pieces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME default current_timestamp,

    piece_cid text not null, -- v1
    piece_padded_size bigint not null,
    piece_raw_size bigint not null,

    complete INTEGER not null default false,
    task_id bigint default null,

    cleanup_task_id bigint default null,

    -- long_term INTEGER not null default false, -- Added in 20240930-pdp.sql

    -- skip INTEGER not null default false, -- Added in 20250505-market-mk20.sql to allow skipping download

    -- NOTE: Following keys were dropped in 20240507-sdr-pipeline-fk-drop.sql
    foreign key (task_id) references harmony_task (id) on delete set null, -- dropped
    foreign key (cleanup_task_id) references harmony_task (id) on delete set null, -- dropped

    unique (piece_cid) -- dropped in 20240930-pdp.sql
    -- unique (piece_cid, piece_padded_size, long_term, cleanup_task_id) -- Added in 20240930-pdp.sql
);

/*
 * This table is used to keep track of the references to the parked pieces
 * so that we can delete them when they are no longer needed.
 *
 * All references into the parked_pieces table should be done through this table.
 *
 * data_url is optional for refs which also act as data sources.
 *
 * Refs are ADDED when:
 * 1. MK12 market accepts a non-offline deal
 *
 * Refs are REMOVED when:
 * 1. (MK12) A sector related to a pieceref: url piece is finalized
 * 2. (MK12) A deal pipeline not yet assigned to a sector is deleted
 *
 */
create table if not exists parked_piece_refs (
    ref_id INTEGER PRIMARY KEY AUTOINCREMENT,
    piece_id bigint not null,

    data_url text,
    data_headers jsonb not null default '{}',

    -- long_term INTEGER not null default false, -- Added in 20240930-pdp.sql

    foreign key (piece_id) references parked_pieces(id) on delete cascade
);
