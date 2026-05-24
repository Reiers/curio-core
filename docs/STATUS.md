# Curio Core — Live status

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11). This document is the single source of truth for "where is it." Updated at every meaningful milestone.

Last updated: **2026-05-24 14:30 CEST** (Day 8 COMPLETE: first on-chain tx via curio-core pipeline, SP registered as calibration provider id 25, CurioChainSched + watchers live, synapse-sdk HTTP compat suite green, runtime-network-aware contracts. #56 P1–P4 shipped, P5 in progress.)

## Day 8 status: COMPLETE

End-to-end on-chain pipeline now closes. Every sub-milestone P1–P7 shipped in a single working day; final commit at 2026-05-24 11:21 CEST.

- **P1 — ethkeys bootstrap + SenderETH wiring + VMBridge configured** (7b1cc00, 174dc86, 943a3bd, ebdd298). curio-core mints its own ETH key on first run, the `SenderETH` task pulls signed payloads through, and `eth_*` traffic routes via the embedded Lantern VMBridge.
- **P2 — first on-chain tx via the curio-core pipeline** (f504592, 8f3cdc8, cee15c9). 1-wei self-transfer landed in calibration block 3742933 from the `admin/test-tx` endpoint. Closed [#17](https://github.com/Reiers/curio-core/issues/17) (timestamp scan fix in harmonysqlite) + [#54](https://github.com/Reiers/curio-core/issues/54) (slice-of-scalar Select).
- **P3 — SP registered as calibration provider id 25** (350e2b4, 93263d4, 523cd13). `curio-core wallet`, `curio-core doctor`, and `curio-core sp register --submit` operator CLIs shipped; 7 PDPv0 capability fields populated on-chain. Closed [#38](https://github.com/Reiers/curio-core/issues/38), [#40 part 1](https://github.com/Reiers/curio-core/issues/40), [#41](https://github.com/Reiers/curio-core/issues/41).
- **P4 — CurioChainSched + 3 watcher handlers wired on tipset sub** (d87bd5f, b636618, 5eec884). Paired with Lantern V1.5.0 `pkg/daemon` HeadChanges wiring; `TaskChainSync` plus the three pdpv0 watchers (`InitProvingPeriodWatch`, `NextProvingPeriodWatch`, `ProveWatch`) consume real tipsets cleanly end-to-end. Closed [#55](https://github.com/Reiers/curio-core/issues/55).
- **P5 — synapse-sdk HTTP compatibility test suite** (aad1d6d). 8 tests + 6 route smoke subtests, opt-in via `CURIO_CORE_URL`. Closed [#13](https://github.com/Reiers/curio-core/issues/13).
- **P6 — runtime-network-aware contract addresses** (aa7ef47). 40+ call-site refactor in the Reiers/curio fork across `pdp/`, `tasks/pdpv0/`, `lib/filecoinpayment/` so PDPVerifier / FilecoinPay / Payments / WarmStorage addresses are resolved from the live chain network at startup instead of being hard-coded. Closed [#46](https://github.com/Reiers/curio-core/issues/46).
- **P7 — mainnet read-only probe + headless config CLI** (7fd5fce, 97f1646). `curio-core probe` now also targets mainnet; `curio-core config show|set|status` provides full headless setup. Closed [#7](https://github.com/Reiers/curio-core/issues/7) + [#8](https://github.com/Reiers/curio-core/issues/8).

Supporting work shipped same day: PDPVerifier v3.4.0 ABI bump in the fork ([#27](https://github.com/Reiers/curio-core/issues/27), 31fccd3), CLI error-scenario smoke matrix ([#50](https://github.com/Reiers/curio-core/issues/50), 2210347), 90 MB binary footprint enforced in CI ([#53](https://github.com/Reiers/curio-core/issues/53), 0be81ab), `pdp_data_set_piece_adds.pdp_pieceref` indexed ([#28](https://github.com/Reiers/curio-core/issues/28), 07a2b45), README rewritten as Hot Storage FoC product spec (7cbdba0).

## #56 status: P1–P4 SHIPPED, P5 IN PROGRESS

The "drive a real PDP proof through the loop" workstream is most of the way through. Indexstore, parked-piece bridge, and the four proving tasks are all wired; only the synapse-sdk-driven signed-`extraData` flow remains.

- **P1 — SQLite-backed indexstore** (be85045). New `internal/sqliteindex` package, 9 unit tests, replaces the upstream YugabyteDB indexstore via the new interface.
- **P2 — indexstore.Backend interface refactor in Reiers/curio fork** (134d23e). Fork now consumes the indexstore via a `Backend` interface so curio-core can plug in `internal/sqliteindex` and upstream stays free to keep its Yugabyte impl.
- **P3.1 — `internal/parkcomplete` task** (d895436). Streaming-upload → `parked_pieces.complete` bridge, 11 unit tests. Closes the gap between the diskstash piece-upload path and the harmonytask piece-park lifecycle.
- **P3.2 — `internal/localpiecepark.Reader`** (69581fd). `pieceprovider.PieceParkBackend` implementation over diskstash, 9 unit tests.
- **P3.3 — cachedreader construction in curio-core** (69581fd). Confirmed `sectorReader == nil` is safe in the cachedreader code path under the pdpv0-only profile.
- **P4 — ProveTask + InitProvingPeriodTask + NextProvingPeriodTask + SaveCache wired** (69581fd). All four proving-cycle tasks live in `pdpwire.BuildChainDeps` + `main.go` extraTasks; harmonytask registry now holds the full PDPv0 proving pipeline (13 task types).
- **P5 — end-to-end signed-`extraData` synapse-sdk flow** — STILL OPEN. Need to drive a proof through the loop with the real synapse-sdk client signing the extraData. In progress now.

## What works today

- `curio-core probe` boots an embedded Lantern, anchors against either calibration or live mainnet gateway, and shuts down cleanly. 25 MB pure-Go binary, `CGO_ENABLED=0`. CI enforces a 90 MB hard upper bound.
- `curio-core run` starts the full daemon: SQLite state DB (auto-migrates) → harmonytask task registry (13 PDPv0 entries: 9 original + ProveTask + InitProvingPeriodTask + NextProvingPeriodTask + SaveCache) → first-run config probe → embedded Lantern with VMBridge → WebUI with `/setup` flow → admin/test-tx + admin endpoints. SIGTERM unwinds cleanly.
- **First on-chain tx through the pipeline:** 1-wei self-transfer landed in calibration block **3742933** via `admin/test-tx`, signed by the curio-core-minted ETH key, sent via SenderETH → Lantern VMBridge.
- **SP registered on calibration as provider id 25.** `curio-core sp register --submit` broadcast the registration with 7 PDPv0 capability fields populated.
- **CurioChainSched + 3 pdpv0 watchers** consume Lantern's HeadChanges sub on every tipset; `TaskChainSync` plus `InitProvingPeriodWatch`, `NextProvingPeriodWatch`, `ProveWatch` all run clean.
- **synapse-sdk HTTP compatibility:** 8 tests + 6 route smoke subtests pass against a running curio-core when `CURIO_CORE_URL` is set.
- **Runtime-network-aware contract addresses.** PDPVerifier (v3.4.0 ABI), FilecoinPay, Payments, WarmStorage resolve from the live chain network at startup; no more compile-time constants for these.
- **Operator CLIs:**
  - `curio-core config show|set|status` — headless first-run setup (no WebUI required)
  - `curio-core wallet ...` — operator ETH wallet management
  - `curio-core doctor` — pre-flight diagnostics
  - `curio-core sp register [--submit]` — calibration/mainnet SP registry registration
- `internal/engine` wraps the SQLite handle + task registry + lifecycle. Single-server (no Peering layer); `harmony_machines` stays a single-row table keyed by HostAndPort.
- `internal/config` owns the `harmony_config` default-layer read/write (TOML-encoded `[Pdp]{MarketAddress, WalletAddress, MinerID}` plus the eth-key state).
- `internal/setupweb` is the CGO-free /setup HTTP handler + middleware.
- `internal/harmonysqlite` applies 14 migrations on `New()`, produces 55 SQLite tables (16 `pdp_*` + 39 infra). Both Select paths + `QueryRowI` route through `scanWithTimeFix`; `(*DB).Select` now also accepts slice-of-scalar destinations.
- `internal/sqliteindex` (#56 P1) is a SQLite-backed `indexstore.Backend` with 9 unit tests; replaces the upstream YugabyteDB indexstore inside the pdpv0 import graph.
- `internal/parkcomplete` (#56 P3.1) bridges streaming uploads to `parked_pieces.complete`; 11 unit tests.
- `internal/localpiecepark` (#56 P3.2) implements `pieceprovider.PieceParkBackend` over diskstash; 9 unit tests.
- `internal/ethclient` + `pdpwire` route `eth_*` calls through the embedded Lantern over `/rpc/v1` with a self-minted token; 6+ extra `eth_*` methods served via VMBridge fallback (Lantern bumps a115cc21ac5d, 9621aa8a2125).
- `Reiers/curio` fork's `tasks/pdp` + `tasks/pdpv0` compile under `CGO_ENABLED=0`, network-aware addresses, PDPVerifier v3.4.0 ABI, `indexstore.Backend` interface seam.
- `Reiers/blooms` (lotus fork): `storage/paths` CGo-bound methods split into `local_cgo.go` + `local_nocgo.go` stubs.
- `web/` is the upstream Curio WebUI with porep/clustering/sealing panels stripped. Behind a `//go:build cgo` shim.
- `CGO_ENABLED=0 go build ./...` is green.
- `CGO_ENABLED=0 go test ./internal/... ./cmd/...` is green.

## What's left

- **#56 P5** — end-to-end signed-`extraData` synapse-sdk flow that actually drives a proof through the full ProveTask → InitProvingPeriodTask → NextProvingPeriodTask → SaveCache loop. In progress.
- WebUI `//go:build cgo` shim still gates pages/pdp/. Carving out gosigar (darwin), `lotus/storage/sealer`, `curio/lib/proofsvc/common`, and `curio/tasks/seal` is deferred behind Day 8 close.
- `internal/pdptests/` still gated behind `//go:build pdp_full_carveout`; drop-gate acceptance deferred.
- No mainnet end-to-end yet; the mainnet SP Registry address is wired (7fd5fce, #7 closed) but actual mainnet registration + proving is a separate workstream.

## Files of record

| File | What it holds |
|---|---|
| `README.md` | Public-facing overview + Hot Storage FoC product spec |
| `docs/PLAN.md` | Day-by-day plan with acceptance criteria |
| `docs/STATUS.md` | This file — live status |
| `docs/SCOPE-DIFF.md` | What's in scope vs upstream Curio |
| `docs/RECON-2026-05-23.md` | Day 1 recon notes (frozen) |
| `docs/SQL-CLASSIFICATION.md` | 118-file classification of upstream Curio migrations |
| `docs/DAY-3-NOTES.md` | Day 3 close: SQL port details, translation patterns, deferred items |
| `docs/DAY-6-NOTES.md` | Day 6 close: lotus/storage carve-out scoping |
| `docs/WEBUI-STRIP-NOTES.md` | Day 3 partial: WebUI strip details + deferred items |
| `internal/harmonysqlite/schema-curio-core/PORT-STATUS.md` | Per-migration port status |

## Commit log highlights

Day 8 + #56 ship train (most recent first):

```
69581fd Wire ProveTask + InitPP + NextPP + SaveCache (#56 P3.2 + P3.3 + P4)
d895436 internal/parkcomplete: streaming-upload -> parked_pieces.complete bridge (#56 P3.1)
134d23e deps: bump Reiers/curio fork to 0d801c5 (indexstore.Backend interface)
be85045 internal/sqliteindex: SQLite-backed indexstore.Backend implementation (#56 P1)
97f1646 config: headless CLI for first-run setup (closes #8)
aa7ef47 pdpwire + synapsecompat: runtime network propagation + on-chain compat tests (closes #46)
7fd5fce sp register: add mainnet SP Registry address (closes #7)
aad1d6d internal/synapsecompat: synapse-sdk HTTP compatibility test suite (closes #13)
5eec884 engine: wire CurioChainSched + 3 pdpv0 watchers on tipset sub (Day 8 P4)
b636618 TaskChainSync runs clean end-to-end on calibration (closes #55 part 2)
523cd13 doctor + sp: two new operator CLIs (closes #41, partial #38)
31fccd3 deps: bump curio fork to 96fea60b85a6 (PDPVerifier v3.4.0 ABI, closes #27)
350e2b4 wallet: operator wallet management CLI (closes #40 part 1)
ebdd298 deps: bump lantern to a115cc21ac5d (6 more eth_* via VMBridge, closes #34)
7b1cc00 Day 8 P1: ethkeys bootstrap + SenderETH wiring + VMBridge config
c9f493f diskstash + pdpwire: first end-to-end piece upload works
```

## Operating notes

- Don't track `internal/harmonysqlite/migrations/` — that path is gitignored because background tooling auto-syncs it from the upstream Curio module cache, which would clobber the curio-core canonical schema at `schema-curio-core/`.
- `cmd/inspect-tables/` is a dev tool: dumps the SQLite schema produced by `harmonysqlite.New()` to stdout. Not shipped in release builds.
- `internal/curiowire/` is scratch space used during the Day 2 + 4 wire-up to surface Curio compilation gaps. May be deleted once the bundle stabilizes.
- `scripts/check-footprint` enforces a 90 MB hard limit on the curio-core binary in CI (#53).
- `admin/test-tx` is the canonical "is the on-chain sender pipeline alive" endpoint; first successful invocation landed in calibration block 3742933.
