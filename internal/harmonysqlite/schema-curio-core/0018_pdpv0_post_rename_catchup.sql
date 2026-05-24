-- 0018_pdpv0_post_rename_catchup.sql
--
-- Full schema audit of post-rename pdp-v0 migrations from upstream,
-- catching up missed ALTER TABLE / ADD COLUMN steps that escaped the
-- original greenfield port. This is the postmortem follow-up from
-- curio-core#62 - the second proof cycle missed because we hit
-- 'no such column: cached_proofgen_failure_count' (fixed in 0016
-- already), and an audit found six MORE missing columns that would
-- have bitten the very next cycle anyway.
--
-- Each ALTER below mirrors a specific upstream migration. Idempotency
-- is achieved by the harmony_schema_migrations tracker (we don't re-
-- run); SQLite doesn't have ADD COLUMN IF NOT EXISTS so this file
-- assumes a clean port-state from 0011 through 0017.
--
-- Upstream migrations covered:
--   20251004-pdp-v0-indexing.sql       (ipni_task_id + needs_ipni)
--   20251027-pdp-v0-filecoin-pay.sql   (rm_message_hash + removed on pdp_data_set_pieces)
--   20260511-pdpv0-ipni-tracking.sql   (indexed_at + advertisement_created_at)

-- ipni_task_id / needs_ipni: the IPNI announce path stores per-piece
-- announcement state on pdp_piecerefs alongside the existing
-- needs_indexing / indexing_task_id pair we already added in 0016.
-- The IPNI task itself isn't wired in curio-core yet (#42 is the
-- adoption tracker), but ProveTask + watchers reference these
-- columns on the read path. Without them, any query joining on
-- pdp_piecerefs and selecting these columns (e.g. several upstream
-- SELECT * shapes) errors out with 'no such column'.
ALTER TABLE pdp_piecerefs
    ADD COLUMN ipni_task_id INTEGER
        REFERENCES harmony_task(id) ON DELETE SET NULL;
ALTER TABLE pdp_piecerefs
    ADD COLUMN needs_ipni INTEGER NOT NULL DEFAULT 0;

-- rm_message_hash / removed: pdp_data_set_pieces tracks the
-- per-piece deletion lifecycle. ProveTask's provePiece query
-- (tasks/pdpv0/task_prove.go:672) SELECTs dsp.removed; without
-- this column the proof generation fails after the randomness +
-- cached_proofgen_failure_count gates have been passed (this is
-- the failure the second proof cycle would have hit if we hadn't
-- audited; surfaced in the postmortem #62).
ALTER TABLE pdp_data_set_pieces
    ADD COLUMN rm_message_hash TEXT DEFAULT NULL;
ALTER TABLE pdp_data_set_pieces
    ADD COLUMN removed INTEGER NOT NULL DEFAULT 0;

-- indexed_at / advertisement_created_at: timestamps used by the IPNI
-- announce loop to track which pieces have been indexed locally and
-- when an advertisement was created. Same shape as needs_ipni - read
-- path references them; needed for any code path that SELECTs * from
-- pdp_piecerefs or that joins on these columns.
ALTER TABLE pdp_piecerefs
    ADD COLUMN indexed_at TEXT DEFAULT NULL;
ALTER TABLE pdp_piecerefs
    ADD COLUMN advertisement_created_at TEXT DEFAULT NULL;

-- Note: pdp_data_sets.terminated_at_epoch is a v1-era column that
-- was supposed to be RENAMED to unrecoverable_proving_failure_epoch
-- in upstream 20260123-pdp-v0-rename-terminated-at-epoch.sql. Our
-- 0011 created the table with the new name AND the old name (we
-- created both in parallel during the greenfield port). The dead
-- column is harmless but worth dropping in a follow-up. Not in
-- this migration because SQLite DROP COLUMN may be slow on large
-- tables and we want to keep this migration fast-applying. Tracked
-- as a separate followup line item from #62.
