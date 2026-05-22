-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20230712-sector_index.sql
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

create table if not exists sector_location
(
    miner_id         bigint    not null,
    sector_num       bigint    not null,
    sector_filetype  int   not null,
    storage_id       varchar not null,
    is_primary       INTEGER,
    read_ts          DATETIME,
    read_refs        int,
    write_ts         DATETIME,
    write_lock_owner varchar,
    constraint sectorlocation_pk
        primary key (miner_id, sector_num, sector_filetype, storage_id)
);

-- TODO: PG-ALTER-COLUMN-SET. SQLite can't ALTER COLUMN SET NOT NULL/DEFAULT in place.
-- Translation strategy: fold the constraint into the original CREATE TABLE,
-- or rebuild the table (CREATE new, INSERT...SELECT, DROP old, RENAME).
-- alter table sector_location     alter column read_refs set not null;

-- TODO: PG-ALTER-COLUMN-SET. SQLite can't ALTER COLUMN SET NOT NULL/DEFAULT in place.
-- Translation strategy: fold the constraint into the original CREATE TABLE,
-- or rebuild the table (CREATE new, INSERT...SELECT, DROP old, RENAME).
-- alter table sector_location     alter column read_refs set default 0;

create table if not exists storage_path
(
    "storage_id"  varchar not null
        constraint "storage_path_pkey"
            primary key,
    "urls"       varchar, -- comma separated list of urls
    "weight"     bigint,
    "max_storage" bigint,
    "can_seal"    INTEGER,
    "can_store"   INTEGER,
    "groups"     varchar, -- comma separated list of group names
    "allow_to"    varchar, -- comma separated list of allowed groups
    "allow_types" varchar,  -- comma separated list of allowed file types
    "deny_types"  varchar, -- comma separated list of denied file types

    "capacity" bigint,
    "available" bigint,
    "fs_available" bigint,
    "reserved" bigint,
    "used" bigint,
    "last_heartbeat" DATETIME,
    "heartbeat_err"  varchar
);

