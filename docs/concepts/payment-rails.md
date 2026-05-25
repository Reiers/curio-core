# USDFC payment rails

Clients pay storage in **USDFC** through **FilecoinPay** payment rails. Each
dataset is backed by one rail: a continuous stream of USDFC from the client (payer)
to the SP (payee) at a per-epoch rate.

## How a rail comes to exist

1. The client opens a `FilecoinPay` rail with this SP as payee, USDFC as token.
2. FilecoinWarmStorageService (FWSS) is the **operator** of the rail — it controls
   the payment rate based on dataset size + price.
3. The client deposits USDFC into FilecoinPay, then sets an operator approval that
   lets FWSS draw from their deposit for this rail.
4. As epochs pass, the rail accumulates a claimable amount =
   `paymentRate × (currentEpoch − settledUpTo)`.

## How curio-core claims it

The singleton harmonytask `PDPv0_PaySettle` runs every 10 minutes. Each cycle:

1. Calls `FilecoinPay.getRailsForPayeeAndToken(our_pdp_addr, USDFC, ...)` to enumerate
   every rail with this SP as payee. Pure on-chain read; no event-log subscription.
2. Upserts each rail into `pdp_payment_rails`. First sight reads `getRail()` for
   payer/operator/validator metadata; re-sights just refresh `terminated` + `end_epoch`.
3. For each non-terminated rail, applies the **eligibility heuristic** (ported from
   upstream `filecoinpayment.SettleLockupPeriod`): only attempt settlement when
   `settledUpTo + min(lockupPeriod - 1day, 7days) ≤ currentEpoch`. This avoids the
   `CannotSettleFutureEpochs` revert that fires when the account-side lockup hasn't
   yet advanced.
4. Eligible rails get `settleRail(railId, currentEpoch)` dispatched via SenderETH.
   The tx hash is recorded on the row + into `filecoin_payment_transactions` for the
   chain-message-watcher to consume.

## Live state

On the dashboard's **Rails** page:

- **Incoming rate** = sum of `paymentRate` across all non-terminated rails (USDFC/epoch).
- **Active rails** = count of non-terminated rows.
- Per-row: payer, status (pending / settled / settled-error / terminated), rate, last
  on-chain settlement tx.

For the actual USDFC balance you've claimed cumulatively, query the SP wallet:

```bash
curio-core wallet list
# Shows tFIL + USDFC balances per row
```

## Termination

When a client terminates a dataset, FWSS calls `terminateRail` on FilecoinPay. The
rail gets `endEpoch` set; from that point the rate stops accruing. Curio-core's
`PDPv0_PaySettle` keeps the row but stops dispatching settle attempts past the end.

A final `settleTerminatedRailWithoutValidation` claim for the rail's residual amount
is **not** wired in V1 — that's a known follow-on. Today, terminated rails simply
stop accruing and any unsettled tail stays in FilecoinPay.

## What can go wrong

| Symptom | Cause | Action |
|---|---|---|
| Rail appears but `PaymentRate` is 0 | FWSS hasn't yet pushed the rate update | Wait one settlement cycle |
| `CannotSettleFutureEpochs` revert on every attempt | Eligibility heuristic is gating | Expected; rails settle on the 7-day grace |
| Rail not appearing at all | Client hasn't opened a rail yet | Verify via direct contract call to `getRailsForPayeeAndToken` |
| Settle tx broadcasts but never lands | Lantern lost chain head | Restart curio-core; check `/tasks` for `MessageWatcherEth` advancement |

The dashboard's **Rails** page is the operator-friendly view; the SQL ground truth
lives in `pdp_payment_rails`.
