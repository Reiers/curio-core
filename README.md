<div align="center">

<img src="docs/assets/curio-core-mark-256.png" alt="Curio Core" width="120" />

# CURiO core

**PDP-only Curio + embedded Lantern, as a single pure-Go binary.**

*One binary. Zero CGo. Single-server PDP without the Lotus burden.*

[![License: Apache 2.0 OR MIT](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue.svg)](#license)

</div>

---

> **Status: pre-alpha.** Branch cut 2026-05-23. Not yet runnable end-to-end; PDP integration in progress. Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11).

## What this is

Curio Core is a slimmed Curio with **PDP-only task scope**, bundled with an embedded [Lantern](https://github.com/Reiers/lantern) light node, distributed as **one static binary**.

Drop-in target: an operator who wants to run PDP and nothing else, on a single box, without standing up a Filecoin node infrastructure first.

## Design philosophy

Curio Core is intentionally minimal. Three rules:

1. **Pure-Go.** Zero CGo dependencies, no Rust toolchain, no `filecoin-ffi` linkage. The whole stack — Lantern, harmonydb, harmonytask, PDP tasks — runs in the Go heap.
2. **Single-server.** No clustering, no peer coordination, no multi-node DB. SQLite via [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) is the storage layer. The single-writer constraint is correct for the PDP-only operator profile.
3. **Lantern stays a node.** Lantern is the general-purpose Filecoin light node. Curio Core depends on it the same way any other Go program would. The dependency direction is always `curio-core → lantern`, never the reverse.

## What gets bundled

| Component | Source | Role |
|---|---|---|
| [Lantern](https://github.com/Reiers/lantern) | embedded as Go library | Lotus-compatible JSON-RPC backend (chain head, state reads, message submission). 40 MB pure-Go runtime. |
| Curio PDP tasks | `Reiers/curio` (forked from `filecoin-project/curio` `integ/task`) | `tasks/pdp` (v1) + `tasks/pdpv0` (v0). Both protocol versions supported. |
| harmonytask | same fork | Andy's three-layer task system (Scheduler + Peering + DB). Curio Core runs Scheduler + DB only, skips Peering (single-server). |
| harmonysqlite | this repo, `internal/harmonysqlite` | Drop-in replacement for upstream `harmonyquery` (Postgres-only). SQLite-backed via `modernc.org/sqlite`. |
| `PieceReader` interface | this repo, `internal/pieceio` | Carves the file-IO method PDP needs out of Curio's `lib/ffi.SealCalls`, keeping the bundle CGo-free. |

## What's stripped vs upstream Curio

- Sealing pipeline (`sealsupra`, `seal`, `winning`, `window`, `snap`, `sdr`)
- Boost / `storage-market` task surfaces (non-PDP retrieval)
- Multi-node clustering (we're single-server by design)
- WebUI panels for sealing
- Configuration layers — one default layer (PDP), first-run prompts for the rest

## Why this exists

1. **Adoption.** Today the PDP entry cost is "stand up Lotus (76 GB snapshot), Yugabyte (3-node cluster recommended), Boost, Curio." Curio Core makes it "run this 80 MB binary."
2. **Pure-Go all the way down.** Lantern proved that the chain layer can be CGo-free; SQLite via modernc keeps the task layer the same.
3. **Lantern proves out.** Curio + Lantern as failover backup is interesting; Curio + Lantern as the **primary** stack for a PDP-only operator is the load-bearing claim.

## Status at a glance

| Day | Item | State |
|---|---|---|
| 1 | Recon: scope diff, transitive import map | ✓ shipped (`docs/SCOPE-DIFF.md`) |
| 2 | Bones: Lantern embed, PieceReader interface | ✓ shipped (`cmd/curio-core probe` boots an embedded Lantern + anchors mainnet) |
| 3 | SQLite scaffold | ✓ shipped (`internal/harmonysqlite`) |
| 3-4 | CGo carveout in Reiers/curio fork | ✓ shipped (`tasks/pdp` compiles under `CGO_ENABLED=0`) |
| 4 | Core SQL migrations (6 files: harmonytask, machine, message, config) | ✓ shipped (`schema-curio-core/0001-0006`) |
| 4-5 | PDP-domain SQL migrations (29 files w/ triggers + functions) | partial (3 of 29; see `internal/harmonysqlite/schema-curio-core/PORT-STATUS.md`) |
| 6-7 | PDP test port | scaffolded; gated behind `pdp_full_carveout` build tag (needs lotus/storage/paths + gosigar carveout to compile) |
| 8 | Calibration demo | pending |

## Try it

The only thing that currently works end-to-end is the embedded Lantern probe:

```sh
go install github.com/Reiers/curio-core/cmd/curio-core@main
curio-core probe --data-dir /tmp/cc --timeout 30s
```

Sample output:
```
Anchored:
  epoch:       6039409
  state root:  bafy2bzacedqqc7jwp2v6xpobrxkahjm7mooapk3knfkzuk65biwiljk6ak5l2
  F3 instance: 466453
Stopped cleanly.
```

The full `curio-core run` command (with PDP tasks live) is pending the rest of the SQL port + the lotus carveout.

## Brand

Mark and wordmark are derived from the parent Curio brand: the original is a layered isometric cube with a teal accent slit; Curio Core reduces to a single rhombus (the front face) with a teal dot at the geometric center (literally "the core"). Same teal accent `#22BFC4` carried through to the wordmark's lowercase `i`.

Assets live in [`docs/assets/`](docs/assets/). Parent-brand reference materials are preserved in [`docs/assets/parent/`](docs/assets/parent/).

## License

Same as Lantern: Apache 2.0 OR MIT, contributor's choice.
