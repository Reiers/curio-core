-- 0017_drop_legacy_v1_artifacts.sql
--
-- Schema-port audit pass after closing curio-core#56 P5: drop the
-- legacy v1 (pre-rename) tables, triggers, and dead columns. Upstream's
-- 20250730-pdp-v0-rename.sql RENAMES the pdp_proof_sets / pdp_proofset_*
-- tables; we created BOTH old + new in our greenfield port (the v1 set
-- in 0010, the v0 set in 0011). The v1 tables stayed empty (zero rows
-- in 5 weeks of live calibration runs); their triggers fire on empty
-- tables and produce no behavior change.
--
-- This migration cleans them up so future readers don't get confused
-- about which set is canonical, and so any future code path that
-- accidentally references the legacy names fails loudly instead of
-- writing to a dead table.
--
-- Safety: each DROP is gated on the artifact existing. Idempotent across
-- re-runs.

-- Drop legacy triggers first (they reference the legacy tables we're
-- about to drop). DROP TRIGGER IF EXISTS is a no-op when the trigger
-- doesn't exist.
DROP TRIGGER IF EXISTS pdp_proofset_root_insert;
DROP TRIGGER IF EXISTS pdp_proofset_root_delete;
DROP TRIGGER IF EXISTS pdp_proofset_root_update_dec;
DROP TRIGGER IF EXISTS pdp_proofset_root_update_inc;
DROP TRIGGER IF EXISTS pdp_proofset_create_message_status_change;
DROP TRIGGER IF EXISTS pdp_proofset_add_message_status_change;

-- Drop legacy tables. Each is unused (zero rows in any live deployment
-- where curio-core has ever run; the v0-renamed equivalents are what the
-- code path actually exercises). DROP TABLE IF EXISTS is a no-op when
-- the table doesn't exist.
DROP TABLE IF EXISTS pdp_proofset_root_adds;
DROP TABLE IF EXISTS pdp_proofset_roots;
DROP TABLE IF EXISTS pdp_proofset_creates;
DROP TABLE IF EXISTS pdp_proof_sets;

-- Drop the dead pdp_piecerefs.proofset_refcount column. Triggers
-- maintain data_set_refcount (the v0-renamed sibling); proofset_refcount
-- defaults to 0 and is never written. SQLite supports DROP COLUMN since
-- 3.35 (modernc.org/sqlite ships 3.45+), so no table-rebuild dance
-- needed. Idempotent: SQLite errors if the column already-not-exists;
-- gate by checking pragma_table_info via a wrapping DO-block equivalent.
-- Since SQLite has no procedural blocks like Postgres, we trust the
-- migration runner to skip this file on re-application (the migration
-- table records names) and just issue the ALTER unguarded.
ALTER TABLE pdp_piecerefs DROP COLUMN proofset_refcount;
