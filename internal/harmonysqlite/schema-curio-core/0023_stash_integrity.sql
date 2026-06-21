-- 0023_stash_integrity.sql
--
-- Stash-integrity tracking (curio-core#89).
--
-- In curio-core's single-node shape, the stash file IS long-term storage:
-- once parked_pieces.complete flips to 1, the bytes are expected to stay on
-- disk forever. Nothing currently detects the case where a complete=1 row's
-- backing stash file has vanished (operator wiped the dir, a stale-.tmp
-- cleanup deleted live data, disk corruption, etc). When a proving window
-- later challenges that piece, ProveTask faults -> on mainnet a fault is a
-- real economic penalty against the dataset.
--
-- The stash-integrity sweep (internal/stashintegrity) os.Stat()s each
-- complete=1 piece's stash file and records the first time it was found
-- missing here. The flag is CLEARED automatically if the file reappears.
-- We mark, never delete: deleting the row could cascade
-- (parked_piece_refs ON DELETE CASCADE) and destroy the dataset's only
-- record. Marking is reversible and lets the operator decide.

ALTER TABLE parked_pieces ADD COLUMN integrity_missing_at TEXT;

-- Partial index so the sweep + dashboard can cheaply count/list the
-- currently-broken pieces without scanning every row.
CREATE INDEX IF NOT EXISTS parked_pieces_integrity_missing_idx
    ON parked_pieces (integrity_missing_at)
    WHERE integrity_missing_at IS NOT NULL;
