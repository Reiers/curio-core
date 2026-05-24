-- curio-core minimal alerts table (Reiers/curio-core#48).
--
-- Scope (V0): persistent, deduped, ack-able alerts emitted from pdpv0 tasks
-- and infrastructure subsystems. Read via /admin/alerts JSON endpoint.
-- Out of scope: webhook delivery, dashboard integration, multi-severity policy,
-- alert routing. Those land alongside #39 (SP dashboard) and the future webhook
-- config.

CREATE TABLE IF NOT EXISTS curio_alerts (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Dedup key: stable hash of source + identifying params. Two emits with the
    -- same fingerprint update the existing row (count++, last_seen_at, refresh
    -- context_json) instead of inserting a new row.
    fingerprint     TEXT    NOT NULL UNIQUE,

    -- 'warning' | 'error' | 'critical'. Free-form for now; we will firm up the
    -- vocabulary if/when we wire alert routing.
    severity        TEXT    NOT NULL,

    -- Subsystem or task identifier, e.g. 'pdpv0/prove', 'pdpv0/lifecycle',
    -- 'message_waits_eth/failed_tx', 'sender_eth/dispatch'.
    source          TEXT    NOT NULL,

    -- Human-readable single-line message.
    message         TEXT    NOT NULL,

    -- JSON object with structured fields (dataSetId, pieceId, txHash, etc.).
    -- Stored as TEXT for SQLite portability; serialized at emit time.
    context_json    TEXT    NOT NULL DEFAULT '{}',

    -- Unix epoch milliseconds (SQLite has no native timestamptz; ms keeps
    -- ordering stable and avoids tz issues).
    first_seen_at   INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,

    -- Number of times this fingerprint has been emitted (1 on insert,
    -- incremented on each dedup-hit).
    count           INTEGER NOT NULL DEFAULT 1,

    -- Acknowledgement state. ack via POST /admin/alerts/:id/ack.
    acked           INTEGER NOT NULL DEFAULT 0,  -- 0 = unacked, 1 = acked
    acked_at        INTEGER                       -- ms; NULL when unacked
);

CREATE INDEX IF NOT EXISTS curio_alerts_severity_idx
    ON curio_alerts (severity, acked, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS curio_alerts_source_idx
    ON curio_alerts (source, last_seen_at DESC);
