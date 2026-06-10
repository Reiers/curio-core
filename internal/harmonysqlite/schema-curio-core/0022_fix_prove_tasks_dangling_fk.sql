-- 0022_fix_prove_tasks_dangling_fk.sql
--
-- 0017_drop_legacy_v1_artifacts dropped pdp_proof_sets but left
-- pdp_prove_tasks behind, still declaring:
--
--   proofset INTEGER NOT NULL REFERENCES pdp_proof_sets(id) ON DELETE CASCADE
--
-- Two bugs follow:
--
-- 1. DANGLING FK. With foreign_keys=ON, SQLite resolves FK targets at
--    statement prepare. Any DML that touches pdp_prove_tasks -- including
--    the FK-cascade scan triggered by DELETE FROM harmony_task in
--    harmonytask's completion recorder -- fails with
--    "no such table: main.pdp_proof_sets". On a fresh install this means
--    EVERY task completion fails to record and retries forever
--    (observed live: harmonytask task_type_handler.go:482 error spam on
--    a clean calibration boot).
--
-- 2. STALE COLUMN NAME. Upstream's 20250730-pdp-v0-rename.sql renamed
--    proofset -> data_set; tasks/pdpv0/task_prove.go inserts
--    (data_set, task_id). Against the v1-shape table that INSERT fails
--    with "no such column", so ProveTask scheduling breaks too.
--
-- Fix: rebuild pdp_prove_tasks in the v0 vocabulary, FK'd to
-- pdp_data_sets. The table is transient scheduling state (rows live only
-- while a prove task is in flight), so the copy is best-effort: on any
-- broken deployment the table is empty (nothing could insert), and on a
-- pre-0017 deployment rows are in the old column shape.
--
-- NOTE for future drops: when dropping a referenced table, always grep
-- schema files for "REFERENCES <table>" first. 0017 missed this one.

CREATE TABLE IF NOT EXISTS pdp_prove_tasks_v0 (
    data_set INTEGER NOT NULL REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    task_id  INTEGER NOT NULL REFERENCES harmony_task(id) ON DELETE CASCADE,
    PRIMARY KEY (data_set, task_id)
);

-- Copy any in-flight rows (old column name "proofset" -> "data_set").
-- On post-0017 fresh installs the source table exists but is empty;
-- the SELECT lists the legacy column, which still exists in the legacy
-- table shape this migration replaces.
INSERT OR IGNORE INTO pdp_prove_tasks_v0 (data_set, task_id)
    SELECT proofset, task_id FROM pdp_prove_tasks;

DROP TABLE pdp_prove_tasks;

ALTER TABLE pdp_prove_tasks_v0 RENAME TO pdp_prove_tasks;
