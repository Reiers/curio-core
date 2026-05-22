-- Auto-translated from Postgres -> SQLite for Curio Core.
-- Source: github.com/filecoin-project/curio harmony/harmonydb/sql/20230719-harmony.sql
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
-- Flagged constructs: COMMENT ON
--

/* For HarmonyTask base implementation. */

CREATE TABLE IF NOT EXISTS harmony_machines (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    last_contact DATETIME NOT NULL DEFAULT current_timestamp,
    host_and_port varchar(300) NOT NULL, 
    cpu INTEGER NOT NULL, 
    ram BIGINT NOT NULL, 
    gpu REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS harmony_task (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
    initiated_by INTEGER,     
    update_time DATETIME NOT NULL DEFAULT current_timestamp,
    posted_time DATETIME NOT NULL,
    owner_id INTEGER REFERENCES harmony_machines (id) ON DELETE SET NULL, 
    added_by INTEGER NOT NULL,
    previous_task INTEGER,
    name varchar(16) NOT NULL
    -- retries INTEGER NOT NULL DEFAULT 0 -- added later
    -- unschedulable INTEGER DEFAULT FALSE -- added in 20250111-machine-maintenance.sql
    -- restart_request DATETIME -- added in 20250818-restart-request.sql
);
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task.initiated_by IS 'The task ID whose completion occasioned this task.';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task.owner_id IS 'The foreign key to harmony_machines.';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task.name IS 'The name of the task type.';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task.owner_id IS 'may be null if between owners or not yet taken';
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task.update_time IS 'When it was last modified. not a heartbeat';

CREATE TABLE IF NOT EXISTS harmony_task_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,  
    task_id INTEGER NOT NULL, 
    name VARCHAR(16) NOT NULL,
    posted DATETIME NOT NULL, 
    work_start DATETIME NOT NULL, 
    work_end DATETIME NOT NULL, 
    result INTEGER NOT NULL, 
    err varchar
);
-- (PG COMMENT ON stripped) COMMENT ON COLUMN harmony_task_history.result IS 'Use to detemine if this was a successful run.';

CREATE TABLE IF NOT EXISTS harmony_task_follow (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,  
    owner_id INTEGER NOT NULL REFERENCES harmony_machines (id) ON DELETE CASCADE,
    to_type VARCHAR(16) NOT NULL,
    from_type VARCHAR(16) NOT NULL
);

CREATE TABLE IF NOT EXISTS harmony_task_impl (
    id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,  
    owner_id INTEGER NOT NULL REFERENCES harmony_machines (id) ON DELETE CASCADE,
    name VARCHAR(16) NOT NULL
);