CREATE TABLE IF NOT EXISTS harmony_machine_details (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    machine_id INTEGER NOT NULL REFERENCES harmony_machines(id) ON DELETE CASCADE,
    tasks TEXT NOT NULL,
    layers TEXT NOT NULL,
    startup_time TEXT NOT NULL,
    miners TEXT NOT NULL DEFAULT '',
    machine_name TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS harmony_machine_details_machine_id_idx
    ON harmony_machine_details(machine_id);
