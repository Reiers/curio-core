## Curio Core v0.1.0-beta.2

> [!WARNING]
> **BETA — early mainnet.** This build has run end-to-end on **Filecoin mainnet** (SP registration, USDFC funding, payments, dataset creation, piece upload, and the proving loop), but it is young. Run it with a wallet you control and funds you can account for, watch the dashboard, and expect rough edges. The proving loop in particular is still being hardened window-by-window on a live dataset.

## ✨ Overview

Curio Core is a PDP-only Curio fused with an embedded [Lantern](https://github.com/Reiers/lantern) pure-Go Filecoin light client, shipped as a single CGO-free binary. No filecoin-ffi, no Rust, no separate Lotus, no Postgres/Yugabyte — SQLite for state, Lantern for chain. The goal is turnkey hot storage that runs on a laptop or a regular desktop.

`v0.1.0-beta.2` is a **proving-reliability and operator-honesty** release. Since `beta.1` (the first mainnet end-to-end), this build hardens the proving-period scheduler against a real on-chain wedge, and overhauls the dashboard to tell the operator the *truth* about what is happening on-chain — including a new Messages/mpool view and an alert path for on-chain transaction reverts.

| Field | Value |
|---|---|
| Version | v0.1.0-beta.2 |
| Type | **Beta, early mainnet** |
| Compare | [v0.1.0-beta.1...v0.1.0-beta.2](https://github.com/Reiers/curio-core/compare/v0.1.0-beta.1...v0.1.0-beta.2) |
| Build | Go 1.26+ required to build from source; `CGO_ENABLED=0` |
| Chain backend | Embedded Lantern (pure-Go light client) |
| Network | Mainnet-capable (early) |
| Footprint | ~100 MB single binary |

## ⭐ Highlights

### 🔧 Proving-period scheduler hardening — `nextProvingPeriod` accept-window clamp

After a missed or recovered proving window, a dataset's on-chain state can briefly become inconsistent (last-proven epoch momentarily ahead of next-challenge epoch). In that state the on-chain `NextPDPChallengeWindowStart` view can return a challenge epoch that the state-changing `nextProvingPeriod` call then **rejects** with `InvalidChallengeEpoch`, reverting the tx and wedging the dataset in a retry loop that never re-arms proving.

Curio Core now **validates the next challenge epoch against the contract's actual accept-window via `eth_call` before broadcasting**, and clamps it into the reported `[min, max]` window when the view has drifted. Non-window reverts fall through unchanged, so the guard can only help — never block a previously-working schedule. Verified on mainnet: a live wedged dataset was recovered (a drifted epoch was clamped back into range and the tx landed successfully).

### 📨 New Messages / mpool view

The dashboard gains a **Messages** page showing pending (in-flight) transactions and confirmed history with honest per-tx status — **`success` vs `REVERTED`** — plus sender, nonce, and block. A reverted transaction is shown in red with a banner count. Operators can no longer mistake "the task ran" for "the transaction landed."

### 🚨 On-chain revert alerts

The alerts poller previously only watched task-level failures. But the dangerous case is a task that reports **success** (it broadcast a tx) while the **tx reverts on-chain**. Curio Core now watches `message_waits_eth` for confirmed-but-reverted transactions and raises an alert per revert (critical for proving-path reasons), with the tx hash, reason, and nonce. The Alerts page was also corrected to read the live alerts table it had been ignoring.

### ✅ Honest proving status

The Datasets page no longer shows a flat "healthy" for any dataset with zero recorded failures. It now reads **`getDataSetLastProvenEpoch` on-chain** per dataset and derives a truthful status — `proven`, `awaiting window`, `prove overdue`, `never proven`, `wedged`, or `terminated` — with a new **Last proven (on-chain)** column. The Overview's prove panel was relabeled to make clear it counts prove *tasks* (proof generated + broadcast), not on-chain landings.

### 🛠️ Operator actions

New one-click dashboard actions:

- **Retry proving** — re-arm the proving schedule for stuck datasets (the scheduler + accept-window clamp then pick a valid challenge epoch). The one-click form of a manual un-wedge.
- **Clear stale** — remove orphaned, superseded message-send records (nonce already below the on-chain nonce, never wait-tracked) that otherwise linger as phantom "pending" entries. Nonce-guarded; confirmed history is left intact.
- **Ack / Clear alerts** — acknowledge a single alert or all open alerts.

## 🧱 Upgrade notes

- Drop-in binary replacement. SQLite state is forward-compatible from `beta.1`.
- The proving-loop hardening is engine-side only; it does not change proof generation. Existing proofs remain valid.
- This release pairs best with a recent embedded Lantern that returns full `eth_getTransactionReceipt` fields (`from`/`to`/`effectiveGasPrice`); transaction cost display in the Messages view depends on it and is a follow-up.

## 🔭 Known gaps / next

- Transaction **cost columns** (effectiveGasPrice × gasUsed → FIL) in the Messages view are stubbed pending the Lantern receipt-field backport.
- The proving loop is still being hardened on a live mainnet dataset window-by-window.
