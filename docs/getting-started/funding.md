# Funding the wallet

A Curio Core SP needs **gas** to broadcast proofs and rail settlements, plus a small
**FIL deposit** when clients open new datasets.

## What costs FIL?

| Action | Approx. cost | Frequency |
|---|---|---|
| `PDPv0_Prove` tx | gas only (~0.0005 tFIL) | Per dataset per proving window (~every 2h) |
| `PDPv0_InitPP` tx (next proving period advance) | gas only | After every prove |
| `settleRail` tx | gas only | Per rail per ~7-day grace cycle (or more on settlement-heavy SPs) |
| `createDataSet` (client-initiated) | gas + 0.1 FIL cleanup deposit | Per new dataset (refunded on cleanup) |
| Service Registry register/deregister | 5 FIL each | Once when listing your SP |

The 5 FIL Service Registry registration is the **only** large up-front cost. Everything
else is per-tx gas.

## Recommended starting balances

| Network | Suggested tFIL/FIL on bootstrap | Notes |
|---|---|---|
| Calibration | 0.5 tFIL | Easy to top up via the calibration faucet at <https://faucet.calibration.fildev.network/> |
| Mainnet | 5–10 FIL | Headroom for Service Registry + a few months of gas. Top up before it drops below ~1 FIL. |

## Sending tFIL/FIL to the SP

The PDP wallet address is shown in the dashboard topbar and in the `Wallets` page. Send
testnet FIL to it from the calibration faucet, or mainnet FIL from your Filecoin wallet.

## USDFC

USDFC is the ERC-20 stablecoin that storage rails are paid in. It shows up on the SP
side in two ways:

1. **Accumulating in** — as the `PDPv0_PaySettle` task claims accrued rail funds, USDFC
   lands in the SP wallet. This is the normal revenue path; you don't send it out.
2. **A small lockup on dataset creation** — the Filecoin Warm Storage Service requires a
   small USDFC deposit/lockup when a dataset is opened. On a self-driven mainnet bring-up
   (you create your own first dataset to prove the loop), the wallet needs a couple of
   USDFC up front, **before** `createDataSet`, or the tx reverts with
   `InsufficientLockupFunds`.

::: tip Check readiness before you spend gas
`curio-core doctor` runs a USDFC preflight and prints `READY` / `NOT-READY` with the
exact remediation, so you don't burn gas on a reverting `createDataSet`.
:::

### Self-funding USDFC (headless)

There is **no native FIL→USDFC liquidity on Filecoin** worth relying on, so Curio Core
can bridge USDC from an L1/L2 (Ethereum, Base, Arbitrum, Optimism, Polygon) into USDFC on
Filecoin, signed by the SP's own key — no browser, no manual DEX clicking:

```bash
# quote only (default)
curio-core wallet get-usdfc --amount 3 --from-chain base

# execute the bridge
curio-core wallet get-usdfc --amount 3 --from-chain base --submit
```

This needs USDC on the chosen source chain plus that chain's RPC in
`CURIO_RPC_<CHAIN>` (e.g. `CURIO_RPC_BASE`), and a Squid integrator id in
`CURIO_SQUID_INTEGRATOR_ID`. The SP's PDP wallet is used as both the source sender and
the Filecoin receiver.

### Moving USDFC out

If you need to move USDFC out (e.g. to consolidate balances or convert to FIL):

```bash
curio-core wallet send --asset usdfc <to-address> <amount>
```

See the [wallet management page](/operating/wallets) for the full sequence.

## Monitoring balance

The dashboard's **Wallets** page shows live tFIL + USDFC balances per wallet, read via
the embedded Lantern. If the PDP wallet drops below ~0.1 tFIL on calibration, proofs
start failing with `insufficient funds` — top up before that happens.

Alerts on low balance are
[tracked in #39](https://github.com/Reiers/curio-core/issues/39) but not yet wired.
