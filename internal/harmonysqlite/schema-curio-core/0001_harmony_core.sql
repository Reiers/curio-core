-- Curio Core's own SQLite schema for harmonytask. See ../migrations/PORT-STATUS.md.

CREATE TABLE IF NOT EXISTS harmony_machines (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    last_contact TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    host_and_port TEXT NOT NULL,
    cpu INTEGER NOT NULL,
    ram INTEGER NOT NULL,
    gpu REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS harmony_task (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    initiated_by INTEGER,
    update_time TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    posted_time TEXT NOT NULL,
    owner_id INTEGER REFERENCES harmony_machines(id) ON DELETE SET NULL,
    added_by INTEGER NOT NULL,
    previous_task INTEGER,
    name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS harmony_task_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    posted TEXT NOT NULL,
    work_start TEXT NOT NULL,
    work_end TEXT NOT NULL,
    result INTEGER NOT NULL,
    err TEXT
);

CREATE TABLE IF NOT EXISTS harmony_task_follow (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER NOT NULL REFERENCES harmony_machines(id) ON DELETE CASCADE,
    to_type TEXT NOT NULL,
    from_type TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS harmony_task_impl (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_id INTEGER NOT NULL REFERENCES harmony_machines(id) ON DELETE CASCADE,
    name TEXT NOT NULL
);
