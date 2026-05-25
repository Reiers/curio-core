# Settlement & USDFC

See [Concepts → Payment rails](/concepts/payment-rails) for the model. This page is the
**operator's** view: what to expect, what to monitor, when to intervene.

## What happens automatically

Every 10 minutes, the singleton task `PDPv0_PaySettle` runs and:

1. Discovers all rails on `FilecoinPay` with this SP as payee (USDFC token).
2. Upserts each rail into `pdp_payment_rails`.
3. For eligible non-terminated rails (those past the 7-day grace window since last
   settle), dispatches `settleRail(railId, currentEpoch)` via SenderETH.
4. Records the tx hash on the row.

You don't have to do anything for accumulated USDFC to land in the SP wallet. The loop
is autonomous.

## Force a settle cycle

```sql
UPDATE harmony_task_singletons
SET run_now_request = 1
WHERE task_name = 'PDPv0_PaySettle';
```

The singleton scheduler picks up the flag within ~30 seconds and re-runs the task.
Useful for diagnosing whether a settle is failing because of state or because the loop
hasn't fired recently.

## Monitor your USDFC balance

```bash
curio-core wallet list
# Lists every wallet + tFIL + USDFC balance
```

Or on the dashboard, the **Wallets** page shows live USDFC per row.

## Sweep USDFC to a cold wallet

If you want to consolidate accumulated USDFC into a cold wallet:

```bash
curio-core wallet send --asset usdfc 0x<cold-wallet-address> <amount>
```

The send goes via SenderETH, so it's tracked in `message_sends_eth` and confirmed by
`MessageWatcherEth` just like a proof tx.

## Failing settlements

When a `settleRail` call reverts, the row's `last_settle_error` column captures the
revert message. Common patterns:

| Revert | Meaning | Action |
|---|---|---|
| `CannotSettleFutureEpochs(railId, max, attempted)` | We asked past where the account-side lockup is settled to | **Expected** during the 7-day grace; the next cycle will retry |
| `RailInactiveOrSettled(railId)` | Rail has nothing to claim right now | Skip; will refresh on next cycle |
| `RailAlreadyTerminated(railId)` | Rail was terminated by FWSS | Stop settling this rail; capture residual via finalization (planned) |
| Gas estimation revert | Usually the same future-epoch issue | Wait one cycle |

The dashboard's **Rails** page shows a `last attempt err` pill when the row has a
non-null `last_settle_error`. Hover for the full error string.

## Tunables

The settle cadence + grace window is currently hard-coded:

| Constant | Value | Where |
|---|---|---|
| `PollInterval` | 10 minutes | `internal/payments/settle.go` |
| `settleInterval` | `min(lockupPeriod - 1day, 7days)` | per-rail computation |
| Override for testing | `CURIO_CORE_PAYMENTS_MIN_SETTLE_EPOCHS` env var | not documented for production |

Production-tuning recommendations land when we see how rails behave on mainnet.
