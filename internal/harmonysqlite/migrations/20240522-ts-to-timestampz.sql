-- Hand-written override for Curio Core (not a mechanical port).
-- Source reference: github.com/filecoin-project/curio harmony/harmonydb/sql/20240522-ts-to-timestampz.sql
-- See ../../../docs/SQL-CLASSIFICATION.md and DAY-3-NOTES.md for reasoning.
--

-- Original Postgres migration converted TIMESTAMP -> TIMESTAMPTZ on many tables
-- using `ALTER TABLE ... ALTER COLUMN ... TYPE TIMESTAMPTZ USING ... AT TIME ZONE 'UTC'`.
-- SQLite has no timezone-aware datetime type — DATETIME is just stored as TEXT/REAL/INTEGER
-- with NUMERIC affinity. The new tables already use DATETIME (translated from TIMESTAMP),
-- so this migration is a no-op in SQLite. The Go layer must ensure all writes are UTC.
-- (No-op intentionally; the file exists so the migration ordering stays consistent.)
SELECT 1;
