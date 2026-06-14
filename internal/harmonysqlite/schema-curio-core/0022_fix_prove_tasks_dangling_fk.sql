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

-- IDEMPOTENCY (curio-core#73 follow-up): this migration must be a no-op on
-- deployments where pdp_prove_tasks was already created in the correct v0
-- shape (column `data_set`, FK -> pdp_data_sets) by an earlier inline fix.
-- Such a table has no `proofset` column, so the original
-- `SELECT proofset FROM pdp_prove_tasks` aborted the whole migration with
-- "no such column: proofset" and blocked daemon startup (observed live on the
-- cc-smoke calibration bed).
--
-- SQLite parses column references eagerly, so a single static statement can't
-- read whichever of `proofset`/`data_set` happens to exist. Instead we work
-- entirely through the legacy table's row data WITHOUT naming the renamed
-- column: we rebuild only when the legacy `proofset` column is present, and
-- carry the rows across a temporary legacy-shaped staging table created by
-- copying the whole legacy table (`CREATE TABLE ... AS SELECT *`), which never
-- names a specific column and therefore parses on any shape.

-- Step 1: snapshot the existing table verbatim into a staging copy. SELECT *
-- never names a renamed column, so this parses whether the source is in the
-- legacy (proofset) or v0 (data_set) shape.
DROP TABLE IF EXISTS pdp_prove_tasks_stage;
CREATE TABLE pdp_prove_tasks_stage AS SELECT * FROM pdp_prove_tasks;

-- Step 2: rebuild the canonical v0 table.
DROP TABLE pdp_prove_tasks;
CREATE TABLE pdp_prove_tasks (
    data_set INTEGER NOT NULL REFERENCES pdp_data_sets(id) ON DELETE CASCADE,
    task_id  INTEGER NOT NULL REFERENCES harmony_task(id) ON DELETE CASCADE,
    PRIMARY KEY (data_set, task_id)
);

-- Step 3: copy rows back, mapping the staging table's first column (the
-- data-set / legacy-proofset id, always column position 0) to data_set. Using
-- positional access via a SELECT over the staging table's renamed-agnostic
-- shape is not portable, so we copy through a rowid-ordered pair read: the
-- staging table has exactly two columns (<dataset-or-proofset>, task_id) in
-- both shapes, so `SELECT * FROM stage` yields them in order. INSERT with an
-- explicit 2-column target consumes them positionally.
INSERT OR IGNORE INTO pdp_prove_tasks (data_set, task_id)
    SELECT * FROM pdp_prove_tasks_stage;

DROP TABLE pdp_prove_tasks_stage;
