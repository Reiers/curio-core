CREATE TABLE IF NOT EXISTS harmony_task_singletons (
    task_name TEXT PRIMARY KEY NOT NULL,
    task_id INTEGER REFERENCES harmony_task(id) ON DELETE SET NULL,
    last_run_time TEXT NOT NULL DEFAULT '0001-01-01T00:00:00Z'
);
