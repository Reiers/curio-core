-- stash-integrity tracking (Reiers/curio-core#89).
--
-- In curio-core's single-node shape, the diskstash file IS the piece's
-- long-term storage. If a stash file backing a `complete=1` parked_piece
-- disappears (operator cleanup, disk fault, accidental rm), proving that
-- piece will fault on-chain — a real economic penalty on mainnet.
--
-- The stash-integrity sweep (internal/stashsweep) periodically stats the
-- file behind each complete piece. When the file is missing it stamps
-- integrity_missing_at here so downstream (doctor, dashboard, prove path,
-- alerts) can fail fast / surface the broken piece instead of discovering
-- it at challenge time. The column is cleared if the file reappears.
--
-- Nullable, no default => existing rows are unaffected (NULL = healthy).

ALTER TABLE parked_pieces
    ADD COLUMN integrity_missing_at TEXT;

-- Partial index so the "show me broken pieces" query (doctor / dashboard /
-- alerts) is cheap regardless of total piece count.
CREATE INDEX IF NOT EXISTS idx_parked_pieces_integrity_missing
    ON parked_pieces (id) WHERE integrity_missing_at IS NOT NULL;
