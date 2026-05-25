# FAQ

## Is this production-ready?

**No.** Curio Core is pre-alpha. The proof loop works end-to-end on Filecoin
Calibration, but mainnet readiness is the Q3 milestone. Don't run paid workloads on
this yet.

## Why not just use upstream Curio?

Upstream Curio is built for SP clusters: 3+ machines, Yugabyte DB, Lotus full node,
Boost market node, IPNI sidecar. Excellent at that scale.

If you want to run a single-node hot-storage SP — laptop, single VM, "I want to be
my own SP" — upstream is overkill. Curio Core is the answer to "what's the **minimum**
infra to run a paid PDP business?"

The trade-off is intentional: smaller surface, smaller audience.

## Can I run this on a laptop?

Yes. ~88 MB binary, ~200 MB of Lantern state, a few GB of SQLite. The stash directory
(your client piece storage) sets the disk footprint — that's whatever you commit to.

For a laptop SP, 4 CPU / 4 GiB RAM is plenty.

## What does it cost to run?

- **One-time:** 5 FIL for Service Registry registration on mainnet (skippable on
  calibration).
- **Per-tx gas:** ~0.0005 FIL per proof + per settle tx. With 8 datasets and 10-min
  settle cadence, expect ~1 FIL/month in gas overhead.
- **Hosting:** whatever your VM costs ($5–20/mo for a small VPS).

That's it. The big-ticket items in traditional Curio (Lotus host, Yugabyte nodes) are
gone.

## Where does the money come from?

Clients pay you in **USDFC** through FilecoinPay rails. The `PDPv0_PaySettle` task
claims accrued USDFC every 10 minutes; it accumulates in your PDP wallet. Cash out
to FIL or another wallet via `curio-core wallet send`.

The amount depends on how much storage you sell, at what price, to how many clients.
That part is up to you.

## What's the relationship to Lantern?

Lantern is a separate library (also by the same operator) that provides a pure-Go
Filecoin light client. Curio Core **embeds** Lantern — when the curio-core daemon
starts, an in-process Lantern instance handles all chain verification, eth_call
operations, and block dispatch.

You don't manage Lantern separately. Updates to Lantern flow into curio-core via
go.mod bumps.

## Can I run on mainnet?

Not yet. The proof loop is calibration-only today. Mainnet readiness is gated by:

- Audit of the FEVM contract bindings (PDPVerifier, FilecoinPay) against the v3.4.0
  shipped on mainnet
- Mainnet bootstrap quorum for Lantern
- Operator runbook for the first production deployment

See [curio-core#10](https://github.com/Reiers/curio-core/issues/10) for the Q3
checklist.

## What if my SP wallet runs out of FIL?

Proofs and settlement txes start failing with `insufficient funds`. The proof loop's
25-minute retry budget then exhausts and the dataset's `consecutive_prove_failures`
counter ticks up. After 5 consecutive missed proves, FWSS terminates the dataset.

Top up the wallet **before** it drops below ~0.1 FIL. Low-balance alerts are coming
(tracking in [#39](https://github.com/Reiers/curio-core/issues/39)).

## How do clients discover my SP?

The Filecoin Service Registry. After you `curio-core sp register --submit`, your SP's
endpoint URL is published on-chain and clients (e.g. synapse-sdk) find you via the
registry's `getServiceURL(minerID)` call.

## Is the dashboard safe to expose publicly?

**No.** Loopback only today. The dashboard exposes wallet keys (via export endpoint),
runs allowlisted commands (terminal endpoint), and shows live operator metadata.
Production auth is on the roadmap.

For now, reach the dashboard via SSH tunnel:

```bash
ssh -L 14994:127.0.0.1:14994 your-sp-host
```

Then open <http://127.0.0.1:14994/> in your browser.

## Open source license?

Apache 2.0 OR MIT, dual-licensed (your choice). Same as upstream Curio.
