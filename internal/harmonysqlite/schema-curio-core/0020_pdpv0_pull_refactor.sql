-- 0020_pdpv0_pull_refactor.sql
--
-- SQLite mirror of upstream curio PR #1245 migration
-- (harmony/harmonydb/sql/20260521-pdp-v0-pull-refactor.sql).
--
-- PDPv0 pull pipeline refactor. Pull items are now scheduled by
-- unique piece key and written directly to long-term piece storage.
-- Pull item terminal state is explicit so status no longer has to
-- be inferred from StorePiece/parked_pieces task state.
--
-- This migration is structured against the curio-core SQLite schema
-- shape (see cc-smoke / 0011_pdp_v0_dataset.sql etc.), not the
-- upstream Postgres shape. Existing pdp_piece_pull_items columns
-- carried over: fetch_id, piece_cid, piece_raw_size, source_url,
-- task_id, failed, fail_reason. New columns added: complete,
-- created_at, parked_piece_ref, pull_parked_piece_id, retries.
--
-- Upstream uses TIMESTAMPTZ + DEFAULT NOW(); we mirror with TEXT +
-- DEFAULT (datetime('now')). Upstream uses UPDATE...FROM correlated
-- update; we rewrite as UPDATE...SET ...= (SELECT ...).
--
-- Tracks: curio-core#24 (P1 blocker)
-- Upstream PR: https://github.com/filecoin-project/curio/pull/1245

-- Add client_address to pdp_piece_pulls.
ALTER TABLE pdp_piece_pulls
    ADD COLUMN client_address TEXT NOT NULL DEFAULT '';

-- Primary key + column shape change on pdp_piece_pull_items.
-- SQLite cannot DROP/ADD a PRIMARY KEY in place; rebuild the table.
-- We carry the existing seven columns and add the five new ones.

CREATE TABLE pdp_piece_pull_items_new (
    fetch_id              INTEGER NOT NULL REFERENCES pdp_piece_pulls(id) ON DELETE CASCADE,
    piece_cid             TEXT NOT NULL,
    piece_raw_size        INTEGER NOT NULL,
    source_url            TEXT NOT NULL,
    task_id               INTEGER REFERENCES harmony_task(id) ON DELETE SET NULL,
    failed                INTEGER NOT NULL DEFAULT 0,
    fail_reason           TEXT,

    -- new in PR #1245:
    complete              INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    retries               INTEGER NOT NULL DEFAULT 0,
    parked_piece_ref      INTEGER REFERENCES parked_piece_refs(ref_id) ON DELETE SET NULL,
    pull_parked_piece_id  INTEGER REFERENCES parked_pieces(id) ON DELETE SET NULL,

    PRIMARY KEY (fetch_id, piece_cid, source_url),
    CHECK (NOT (complete = 1 AND failed = 1))
);

-- Copy existing rows. For new columns, take defaults except created_at
-- which we'll backfill from the parent pdp_piece_pulls row below.
INSERT INTO pdp_piece_pull_items_new
    (fetch_id, piece_cid, piece_raw_size, source_url, task_id, failed, fail_reason)
SELECT
    fetch_id, piece_cid, piece_raw_size, source_url, task_id, failed, fail_reason
FROM pdp_piece_pull_items;

DROP TABLE pdp_piece_pull_items;
ALTER TABLE pdp_piece_pull_items_new RENAME TO pdp_piece_pull_items;

-- Backfill created_at on existing rows from parent pdp_piece_pulls.
-- Postgres uses UPDATE...FROM; SQLite uses correlated SET-subquery.
-- COALESCE so rows whose parent has NULL created_at keep our default.
UPDATE pdp_piece_pull_items
SET created_at = COALESCE(
    (SELECT pp.created_at FROM pdp_piece_pulls pp WHERE pp.id = pdp_piece_pull_items.fetch_id),
    created_at
);

-- Clear stale task_id references where the harmony_task row no
-- longer exists (orphans from cleanup after task completion).
UPDATE pdp_piece_pull_items
SET task_id = NULL
WHERE task_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM harmony_task ht WHERE ht.id = pdp_piece_pull_items.task_id
);
