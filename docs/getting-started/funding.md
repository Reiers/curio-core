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

## Sending USDFC

USDFC is the ERC-20 token clients pay storage rails in. You don't normally **send**
USDFC out from the SP — instead, USDFC accumulates **into** the SP wallet as the
`PDPv0_PaySettle` task claims accrued rail funds.

If you do need to move USDFC out (e.g. to consolidate balances or convert to FIL), use:

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
