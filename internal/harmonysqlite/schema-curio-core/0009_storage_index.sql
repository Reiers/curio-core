-- Storage index: generic file index used by both sealing (in upstream Curio)
-- and PDP (where it indexes parked pieces under a virtual miner_id = 0).
--
-- Hand-translated from upstream Curio's:
--   20230712-sector_index.sql      (sector_location + storage_path base)
--   20240401-storage-miner-filter.sql (allow_miners / deny_miners on storage_path)
--   20240417-sector_index_gc.sql   (sector_path_url_liveness)
--
-- Translation notes:
--   bool -> INTEGER
--   timestamp(6) -> TEXT (millisecond precision is preserved by the Go layer
--     using ISO-8601 strings; SQLite has no sub-second TIMESTAMP type)
--   ALTER COLUMN SET ...  -> folded into the original CREATE TABLE
--   constraint sectorlocation_pk PRIMARY KEY (...) -> inline PRIMARY KEY

CREATE TABLE IF NOT EXISTS sector_location (
    miner_id         INTEGER NOT NULL,
    sector_num       INTEGER NOT NULL,
    sector_filetype  INTEGER NOT NULL,
    storage_id       TEXT NOT NULL,
    is_primary       INTEGER,
    read_ts          TEXT,
    read_refs        INTEGER NOT NULL DEFAULT 0,
    write_ts         TEXT,
    write_lock_owner TEXT,
    PRIMARY KEY (miner_id, sector_num, sector_filetype, storage_id)
);

CREATE TABLE IF NOT EXISTS storage_path (
    storage_id    TEXT NOT NULL PRIMARY KEY,
    urls          TEXT,                     -- comma-separated
    weight        INTEGER,
    max_storage   INTEGER,
    can_seal      INTEGER,
    can_store     INTEGER,
    groups        TEXT,                     -- comma-separated
    allow_to      TEXT,                     -- comma-separated
    allow_types   TEXT,                     -- comma-separated
    deny_types    TEXT,                     -- comma-separated

    capacity      INTEGER,
    available     INTEGER,
    fs_available  INTEGER,
    reserved      INTEGER,
    used          INTEGER,
    last_heartbeat TEXT,
    heartbeat_err TEXT,

    -- From 20240401-storage-miner-filter.sql:
    allow_miners  TEXT DEFAULT '',          -- comma-separated
    deny_miners   TEXT DEFAULT ''           -- comma-separated
);

CREATE TABLE IF NOT EXISTS sector_path_url_liveness (
    storage_id TEXT NOT NULL,
    url        TEXT NOT NULL,
    last_checked    TEXT NOT NULL,
    last_live       TEXT,
    last_dead       TEXT,
    last_dead_reason TEXT,

    PRIMARY KEY (storage_id, url),
    FOREIGN KEY (storage_id) REFERENCES storage_path (storage_id) ON DELETE CASCADE
);
