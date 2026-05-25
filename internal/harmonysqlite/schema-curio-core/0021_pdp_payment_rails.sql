-- 0021_pdp_payment_rails.sql
--
-- USDFC payment rails tracking for the PDP-as-SP role.
--
-- FilecoinWarmStorageService creates a FilecoinPay rail per client/dataset
-- when storage is provisioned. The SP is the rail's PAYEE and must
-- periodically call FilecoinPay.settleRail(railId, untilEpoch) to claim
-- accumulated USDFC. Without settlement, USDFC stays escrowed inside
-- FilecoinPay's lockup mechanism and never moves to the SP's balance.
--
-- This table is the local cache of "what rails do we know about?",
-- discovered via FilecoinPay.getRailsForPayeeAndToken() polling. Rails
-- terminate when FWSS calls terminateService -> terminateRail; settled
-- amount stops accruing past endEpoch.
--
-- Tracks: curio-core#37 (P1 hot-storage feature).

CREATE TABLE pdp_payment_rails (
    -- Primary key: railId is unique per FilecoinPay deployment.
    rail_id              INTEGER NOT NULL PRIMARY KEY,

    -- Discovered rail metadata. Set on first sight and updated when
    -- modifyRailPayment / RailRateModified events fire.
    payer                TEXT NOT NULL,                  -- 0x... (the client)
    payee                TEXT NOT NULL,                  -- 0x... (us; matches eth_keys role=pdp)
    token                TEXT NOT NULL,                  -- 0x... (USDFC for FWSS)
    operator             TEXT NOT NULL DEFAULT '',       -- 0x... (FWSS contract for our rails)
    validator            TEXT NOT NULL DEFAULT '',       -- 0x... (optional, FWSS-managed)

    -- Settlement bookkeeping. last_settled_epoch advances each time we
    -- successfully call settleRail; settled_total_amount tracks the
    -- accumulated USDFC we've claimed across all settlement calls.
    last_settled_epoch   INTEGER NOT NULL DEFAULT 0,
    settled_total_amount TEXT    NOT NULL DEFAULT '0',   -- big-int decimal, in USDFC base units

    -- Terminal-state mirror of on-chain status. Read from
    -- getRailsForPayeeAndToken's isTerminated flag.
    terminated           INTEGER NOT NULL DEFAULT 0,
    end_epoch            INTEGER NOT NULL DEFAULT 0,

    -- Timestamps for diagnostic UX. last_seen_at is bumped every time
    -- the discovery poll observes this rail; last_settled_at on success.
    created_at           TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen_at         TEXT NOT NULL DEFAULT (datetime('now')),
    last_settled_at      TEXT,

    -- Last attempt diagnostics (for low-traffic operator debugging,
    -- not a load-bearing column).
    last_settle_tx_hash  TEXT,
    last_settle_error    TEXT
);

-- Index for "rails I need to settle" hot-path query: not terminated,
-- last_settled_epoch < current_chain_epoch.
CREATE INDEX idx_pdp_payment_rails_settle_candidates
    ON pdp_payment_rails (last_settled_epoch)
    WHERE terminated = 0;

-- Index for the per-payee discovery cache (in case we ever run as a
-- multi-payee box).
CREATE INDEX idx_pdp_payment_rails_payee
    ON pdp_payment_rails (payee);
