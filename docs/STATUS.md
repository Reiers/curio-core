# Curio Core — Live status

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11). This document is the single source of truth for "where is it." Updated at every meaningful milestone.

Last updated: **2026-05-23 01:55 CEST** (after the Day 5 engine wire-up + first-run config flow).

## What works today

- `curio-core probe` boots an embedded Lantern, anchors against the live mainnet gateway, and shuts down cleanly. 25 MB pure-Go binary, `CGO_ENABLED=0`.
- `curio-core run` starts the full daemon: SQLite state DB (auto-migrates) → harmonytask task registry (21 task types: 12 PDP v1 + 9 PDP v0) → first-run config probe → optional embedded Lantern → WebUI with `/setup` flow. SIGTERM unwinds cleanly. Verified end-to-end against a fresh data-dir.
- `internal/engine` wraps the SQLite handle + task registry + lifecycle. Single-server (no Peering layer); `harmony_machines` stays a single-row table keyed by HostAndPort.
- `internal/config` owns the `harmony_config` default-layer read/write (TOML-encoded `[Pdp]{MarketAddress, WalletAddress, MinerID}`).
- `internal/setupweb` is the CGO-free /setup HTTP handler + middleware (redirect-to-/setup until configured, fall-through afterwards).
- `internal/harmonysqlite` applies 14 migrations on `New()`, produces 55 SQLite tables (16 `pdp_*` + 39 infra), and passes its acceptance test suite (`go test ./internal/harmonysqlite/...`, 8 tests + ~50 subtests).
- `internal/pieceio` defines the `PieceReader` interface that lets us avoid linking `curio/lib/ffi`.
- `Reiers/curio` fork's `tasks/pdp` compiles under `CGO_ENABLED=0` against the `PieceReader` interface (milestone commit a1a4449 in this repo, paired with the corresponding fork commits in Reiers/curio).
- `web/` is the upstream Curio WebUI with porep/clustering/sealing panels stripped (`ab9990f`). Behind a `//go:build cgo` shim until the WebUI's transitive deps (gosigar, lotus storage paths, curio ffi) are carved out.
- `CGO_ENABLED=0 go build ./...` is green.
- `CGO_ENABLED=0 go test ./internal/... ./cmd/...` is green.

## What doesn't work yet

- The harmonytask scheduler goroutine is NOT running. `harmonytask.New` takes a concrete `*harmonydb.DB` (pgx-backed); plugging in our SQLite handle is blocked on a fork-side DB-interface refactor. See `docs/DAY-5-NOTES.md` § "Fork follow-ups" for the concrete patch shape. The engine wraps everything around that hole; once the seam exists, the wire-up is a one-line `harmonytask.New(...)` call.
- PDP v0 task descriptors are static literals (since `tasks/pdpv0` still pulls in gosigar + lotus storage paths). Day 6 swaps them for live `pdpv0.NewXxx().TypeDetails()` harvests.
- `internal/pdptests/` has scaffolds but is gated behind `//go:build pdp_full_carveout` because the test-time transitives still pull in lotus storage paths + gosigar. Day 6.
- No calibration miner running curio-core end-to-end. Day 7-8.

## Files of record

| File | What it holds |
|---|---|
| `README.md` | Public-facing overview + design philosophy |
| `docs/PLAN.md` | Day-by-day plan with acceptance criteria |
| `docs/STATUS.md` | This file — live status |
| `docs/SCOPE-DIFF.md` | What's in scope vs upstream Curio |
| `docs/RECON-2026-05-23.md` | Day 1 recon notes (frozen) |
| `docs/SQL-CLASSIFICATION.md` | 118-file classification of upstream Curio migrations |
| `docs/DAY-3-NOTES.md` | Day 3 close: SQL port details, translation patterns, deferred items |
| `docs/WEBUI-STRIP-NOTES.md` | Day 3 partial: WebUI strip details + deferred items |
| `internal/harmonysqlite/schema-curio-core/PORT-STATUS.md` | Per-migration port status |

## Commit log highlights

```
2d0b569 design: Curio Core mark + wordmark + README rewrite
951613f harmonysqlite: SQL classification + Postgres→SQLite migration port (Day 3)
ea01394 pdptests: scaffold + 0007-0009 schema migrations
948d2aa harmonysqlite: move SQLite schema to schema-curio-core/ (escape upstream auto-sync)
82c63bc harmonysqlite: harmonytask schema migrations + apply runner (Day 4 partial)
ab9990f web: vendor Curio WebUI, strip porep/clustering/sealing panels
a1a4449 milestone: tasks/pdp compiles under CGO_ENABLED=0
0efb5b6 wip: pull in Curio integ/task via Reiers/curio fork (CGo carveout in progress)
775f7c5 ci: GitHub Actions workflow (mirrors lantern's setup)
94348ee harmonysqlite: SQLite scaffold + claim-pattern smoke test (Day 3 down payment)
d67c889 bones: embed Lantern daemon + PieceReader interface (Day 2)
21e590b docs: day 1 recon
7079a1c init: fresh charter (PDP-only Curio + embedded Lantern bundle)
```

## Outstanding questions to Andy (live in issue #11)

See `docs/PLAN.md` § Open questions. Eight concrete items, all in the consolidated comment posted 2026-05-23.

## Operating notes

- Don't track `internal/harmonysqlite/migrations/` — that path is gitignored because background tooling auto-syncs it from the upstream Curio module cache, which would clobber the curio-core canonical schema at `schema-curio-core/`.
- `cmd/inspect-tables/` is a dev tool: dumps the SQLite schema produced by `harmonysqlite.New()` to stdout. Not shipped in release builds.
- `internal/curiowire/` is scratch space used during the Day 2 + 4 wire-up to surface Curio compilation gaps. May be deleted once the bundle stabilizes.
