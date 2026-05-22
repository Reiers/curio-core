<div align="center">

# Curio Core

**PDP-only Curio + embedded Lantern, as a single pure-Go binary.**

*One binary. Zero CGo. Single-server PDP without the Lotus burden.*

[![License: Apache 2.0 OR MIT](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue.svg)](#license)

</div>

---

> **Status: pre-alpha.** Branch cut May 23 2026. Not yet runnable. Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11).

## What this is

Curio Core is a slimmed Curio with PDP-only task scope, bundled with an embedded [Lantern](https://github.com/Reiers/lantern) light node, distributed as one static binary.

Drop-in target: an operator who wants to run PDP and nothing else, on a single box, without standing up a Filecoin node infrastructure first.

## What gets bundled

| Component | Source | Role |
|---|---|---|
| **Lantern** | github.com/Reiers/lantern | Embedded as a Go library. Provides Lotus-compatible JSON-RPC backend. |
| **Curio PDP tasks** | github.com/filecoin-project/curio (`integ/task`) | Both `tasks/pdp` and `tasks/pdpv0`. Everything else stripped. |
| **harmonytask** | github.com/filecoin-project/curio (`integ/task`) | Three-layer task system. We run Scheduler + DB only; skip Peering (single-node). |
| **harmonydb** | Vendored from Curio, ported Postgres → SQLite | Pure-Go SQLite via `modernc.org/sqlite`. Single-server constraint. |

## What's stripped vs upstream Curio

- Sealing pipeline (sealsupra, seal, winning, window, snap, sdr)
- Boost / storage-market task surfaces (non-PDP retrieval)
- Multi-node clustering (we're single-server by design)
- WebUI panels for sealing
- Configuration layers — one default layer (PDP), first-run prompts for the rest

## Why this exists

1. **Adoption.** Today the PDP entry cost is "stand up Lotus (76 GB), Yugabyte (3-node cluster recommended), Boost, Curio." Curio Core makes it "run this 80 MB binary."
2. **Pure-Go all the way down.** Lantern proved that the chain layer can be CGo-free; SQLite via modernc keeps the task layer the same.
3. **Lantern stays a node.** Lantern is the general-purpose Filecoin light node. Curio Core depends on it the same way any other Go program would. Lantern's identity and roadmap are unaffected.

## Project boundaries

- `Reiers/lantern` is the load-bearing Filecoin node. It works standalone for indexers, dashboards, calibration testing, and any other "I need a Lotus-compatible RPC backend" use case. Its README, releases, and docs make no mention of Curio Core.
- `Reiers/curio-core` (this repo) depends on `github.com/Reiers/lantern` as a Go module. The dependency direction is **always** `curio-core → lantern`, never the reverse.

## Roadmap

See [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11) for the full spec + the Curio team's input.

Rough sprint:

1. **Recon** — walk the integ/task PDP graph + DB schema, file-level in/out scope diff.
2. **Bones** — get the bundle compiling: minimum Curio packages + Lantern library + a placeholder DB driver.
3. **SQLite port** — land `modernc.org/sqlite`, port the ~15-25 PDP-relevant migrations.
4. **Tests** — port Curio's PDP tests, fix what breaks.
5. **Demo** — calibration first, then mainnet.

## License

Same as Lantern: Apache 2.0 OR MIT, contributor's choice.
