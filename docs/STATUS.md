# Curio Core - Live status

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11). This document is the single source of truth for "where is it." Updated at every meaningful milestone.

Last updated: **2026-05-24 16:55 CEST** (Day 8 + #56 P5 + #57 + #58 + #59 + #61 + #4 all closed today; daemon running 0 ERROR / 0 WARN on calibration; awaiting proof window at ~17:47 CEST.)

## Day 8 status: COMPLETE

End-to-end on-chain pipeline now closes. Every sub-milestone P1–P7 shipped in a single working day; final commit at 2026-05-24 11:21 CEST.

- **P1 - ethkeys bootstrap + SenderETH wiring + VMBridge configured** (7b1cc00, 174dc86, 943a3bd, ebdd298). curio-core mints its own ETH key on first run, the `SenderETH` task pulls signed payloads through, and `eth_*` traffic routes via the embedded Lantern VMBridge.
- **P2 - first on-chain tx via the curio-core pipeline** (f504592, 8f3cdc8, cee15c9). 1-wei self-transfer landed in calibration block 3742933 from the `admin/test-tx` endpoint. Closed [#17](https://github.com/Reiers/curio-core/issues/17) (timestamp scan fix in harmonysqlite) + [#54](https://github.com/Reiers/curio-core/issues/54) (slice-of-scalar Select).
- **P3 - SP registered as calibration provider id 25** (350e2b4, 93263d4, 523cd13). `curio-core wallet`, `curio-core doctor`, and `curio-core sp register --submit` operator CLIs shipped; 7 PDPv0 capability fields populated on-chain. Closed [#38](https://github.com/Reiers/curio-core/issues/38), [#40 part 1](https://github.com/Reiers/curio-core/issues/40), [#41](https://github.com/Reiers/curio-core/issues/41).
- **P4 - CurioChainSched + 3 watcher handlers wired on tipset sub** (d87bd5f, b636618, 5eec884). Paired with Lantern V1.5.0 `pkg/daemon` HeadChanges wiring; `TaskChainSync` plus the three pdpv0 watchers (`InitProvingPeriodWatch`, `NextProvingPeriodWatch`, `ProveWatch`) consume real tipsets cleanly end-to-end. Closed [#55](https://github.com/Reiers/curio-core/issues/55).
- **P5 - synapse-sdk HTTP compatibility test suite** (aad1d6d). 8 tests + 6 route smoke subtests, opt-in via `CURIO_CORE_URL`. Closed [#13](https://github.com/Reiers/curio-core/issues/13).
- **P6 - runtime-network-aware contract addresses** (aa7ef47). 40+ call-site refactor in the Reiers/curio fork across `pdp/`, `tasks/pdpv0/`, `lib/filecoinpayment/` so PDPVerifier / FilecoinPay / Payments / WarmStorage addresses are resolved from the live chain network at startup instead of being hard-coded. Closed [#46](https://github.com/Reiers/curio-core/issues/46).
- **P7 - mainnet read-only probe + headless config CLI** (7fd5fce, 97f1646). `curio-core probe` now also targets mainnet; `curio-core config show|set|status` provides full headless setup. Closed [#7](https://github.com/Reiers/curio-core/issues/7) + [#8](https://github.com/Reiers/curio-core/issues/8).

Supporting work shipped same day: PDPVerifier v3.4.0 ABI bump in the fork ([#27](https://github.com/Reiers/curio-core/issues/27), 31fccd3), CLI error-scenario smoke matrix ([#50](https://github.com/Reiers/curio-core/issues/50), 2210347), 90 MB binary footprint enforced in CI ([#53](https://github.com/Reiers/curio-core/issues/53), 0be81ab), `pdp_data_set_piece_adds.pdp_pieceref` indexed ([#28](https://github.com/Reiers/curio-core/issues/28), 07a2b45), README rewritten as Hot Storage FoC product spec (7cbdba0).

## #56 status: P1–P4 SHIPPED, P5 IN PROGRESS

The "drive a real PDP proof through the loop" workstream is most of the way through. Indexstore, parked-piece bridge, and the four proving tasks are all wired; only the synapse-sdk-driven signed-`extraData` flow remains.

- **P1 - SQLite-backed indexstore** (be85045). New `internal/sqliteindex` package, 9 unit tests, replaces the upstream YugabyteDB indexstore via the new interface.
- **P2 - indexstore.Backend interface refactor in Reiers/curio fork** (134d23e). Fork now consumes the indexstore via a `Backend` interface so curio-core can plug in `internal/sqliteindex` and upstream stays free to keep its Yugabyte impl.
- **P3.1 - `internal/parkcomplete` task** (d895436). Streaming-upload → `parked_pieces.complete` bridge, 11 unit tests. Closes the gap between the diskstash piece-upload path and the harmonytask piece-park lifecycle.
- **P3.2 - `internal/localpiecepark.Reader`** (69581fd). `pieceprovider.PieceParkBackend` implementation over diskstash, 9 unit tests.
- **P3.3 - cachedreader construction in curio-core** (69581fd). Confirmed `sectorReader == nil` is safe in the cachedreader code path under the pdpv0-only profile.
- **P4 - ProveTask + InitProvingPeriodTask + NextProvingPeriodTask + SaveCache wired** (69581fd). All four proving-cycle tasks live in `pdpwire.BuildChainDeps` + `main.go` extraTasks; harmonytask registry now holds the full PDPv0 proving pipeline (13 task types).
- **P5 - end-to-end signed-`extraData` synapse-sdk flow** - substantially DRIVEN; awaiting proof window. Today's chain of events on calibration:

  | Step | Tx hash | Block | Result |
  |------|---------|-------|--------|
  | SP register (provider id 26) | `0x805f9eb6...4c14` | 3,743,372 | ✅ |
  | USDFC.approve(FilecoinPay, max) | `0x7e98ddca...4376` | 3,743,423 | ✅ |
  | FilecoinPay.deposit(USDFC, self, 1) | `0xb1a5a086...d826` | 3,743,423 | ✅ |
  | FilecoinPay.setOperatorApproval(USDFC, FWSS, ...) | `0x44feba7f...cc37` | 3,743,423 | ✅ |
  | PDPVerifier.createDataSet → **dataSetId 13977** | `0xb00cfc6f...c029` | 3,743,427 | ✅ |
  | PDPVerifier.addPieces (1 piece) | `0x57002786...8463` | 3,743,476 | ✅ |
  | InitProvingPeriod | `0x4bd323df...4267d` | pending | ✅ broadcast |

  Three structural gaps closed during P5:
  - **Lantern `eth_getBlockByNumber` ETH-shape fix** (Lantern v1.5.1, commit 4cda084): `miner` was returning Filecoin-actor strings (`f0143103`) and `hash` fields were CIDs; strict ETH parsers (go-ethereum types.Header) reject these. New `rpc/handlers/ethshape.go` with `EthAddressFromFilecoinIDActor` (0xff || zero(11) || be64(id)) + `EthHashFromCid` (strip canonical DagCBOR+Blake2b-256 prefix). Mirrors lotus/chain/types/ethtypes without taking a lotus dep.
  - **MessageWatcherEth wasn't constructed in curio-core** (commit 08526c5): upstream wires it via deps/deps.go at startup; we skipped it, so every SenderETH tx stayed `pending` in `message_waits_eth` forever. Now constructed in `main.go` after `engine.Start` via new `Engine.TaskEngine()` accessor.
  - **Trigger missing on `pdp_data_set_creates`** (commit 08526c5): schema migration 0010 had the v1-name `pdp_proofset_create_message_status_change` trigger but the v0 rename (20250730-pdp-v0-rename.sql) added a sibling for `pdp_data_set_creates` that never made it into curio-core. Result: even with `tx_status='confirmed'`, `pdp_data_set_creates.ok` stayed NULL and dataset_watch never matched. Added both v0-name triggers to 0011.

  Three SQL portability fixes shipped in support:
  - Reiers/curio fork @2d67414: `tasks/pdpv0/notify_task.go` NOW() → CURRENT_TIMESTAMP
  - Reiers/curio fork @3b77fde: `pdp/handlers_add.go` `WHERE = ANY($2)` → `WHERE IN ($2, $3, ...)`
  - curio-core schema 0011: collapsed duplicate `filecoin_payment_transactions` block

  Two CLIs added for the demo flow:
  - `curio-core demo create-dataset` (commit 1531184) - EIP-712 typed-data for CreateDataSet, signs with pdp wallet, ABI-encodes extraData, POSTs to `/pdp/data-sets`
  - `curio-core demo prepare-client-payments` (commit 08526c5) - drives the USDFC.approve + FilecoinPay.deposit + setOperatorApproval triple
  - `curio-core demo add-pieces` (commit 55d9907) - EIP-712 typed-data for AddPieces, auto-upgrades v1→v2 piece CIDs via `commcid.PieceCidV2FromV1`, looks up clientDataSetId via FilecoinWarmStorageServiceStateView

  `prove_at_epoch=3,743,707` on dataset 13977; proof window opens ~17:47 CEST. Cron reminder set for 17:55 CEST to verify ProveTask fired.

## What works today

- `curio-core probe` boots an embedded Lantern, anchors against either calibration or live mainnet gateway, and shuts down cleanly. 25 MB pure-Go binary, `CGO_ENABLED=0`. CI enforces a 90 MB hard upper bound.
- `curio-core run` starts the full daemon: SQLite state DB (auto-migrates) → harmonytask task registry (13 PDPv0 entries: 9 original + ProveTask + InitProvingPeriodTask + NextProvingPeriodTask + SaveCache) → first-run config probe → embedded Lantern with VMBridge → WebUI with `/setup` flow → admin/test-tx + admin endpoints. SIGTERM unwinds cleanly.
- **First on-chain tx through the pipeline:** 1-wei self-transfer landed in calibration block **3,742,933** via `admin/test-tx`, signed by the curio-core-minted ETH key, sent via SenderETH → Lantern VMBridge.
- **SP registered on calibration as provider id 26** (was 25 before today's state.sqlite wipe rotated the wallet). `curio-core sp register --submit` broadcast the registration at block 3,743,372 with 7 PDPv0 capability fields populated.
- **Dataset 13977 live on calibration FilOzone**, 1 piece added, InitProvingPeriod broadcast, proof window opens ~17:47 CEST (epoch 3,743,707).
- **CurioChainSched + 3 pdpv0 watchers** consume Lantern's HeadChanges sub on every tipset; `TaskChainSync` plus `InitProvingPeriodWatch`, `NextProvingPeriodWatch`, `ProveWatch` all run clean.
- **synapse-sdk HTTP compatibility:** 8 tests + 6 route smoke subtests pass against a running curio-core when `CURIO_CORE_URL` is set.
- **Runtime-network-aware contract addresses.** PDPVerifier (v3.4.0 ABI), FilecoinPay, Payments, WarmStorage resolve from the live chain network at startup; no more compile-time constants for these.
- **Operator CLIs:**
  - `curio-core config show|set|status` - headless first-run setup (no WebUI required)
  - `curio-core wallet ...` - operator ETH wallet management
  - `curio-core doctor` - pre-flight diagnostics
  - `curio-core sp register [--submit]` - calibration/mainnet SP registry registration
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

- **#56 P5** - awaiting proof window. ProveTask + SaveCache + InitProvingPeriodTask + NextProvingPeriodTask are all registered + InitPP already ran with result=1; ProveTask fires at epoch 3,743,707 (~17:47 CEST). Cron reminder set for 17:55 to verify the proof landed cleanly.
- WebUI `//go:build cgo` shim still gates pages/pdp/. Carving out gosigar (darwin), `lotus/storage/sealer`, `curio/lib/proofsvc/common`, and `curio/tasks/seal` is deferred behind Day 8 close.
- `internal/pdptests/` still gated behind `//go:build pdp_full_carveout`; drop-gate acceptance deferred.
- No mainnet end-to-end yet; the mainnet SP Registry address is wired (7fd5fce, #7 closed) but actual mainnet registration + proving is a separate workstream.

## Active follow-on tickets (filed today)

- ~~**[#57](https://github.com/Reiers/curio-core/issues/57)**~~ (closed 16:20 CEST). Three lifecycle hardening pieces:
  - **New tipset-driven `lifecycle_sweeper.go`** (fork commits 1e752f8 + c4d2e5c): runs every ~10-15s, re-arms data sets where ProveTask MaxFailures stranded them in the `prove_at_epoch IS NOT NULL AND challenge_request_msg_hash IS NULL` shape; surfaces stuck `pdp_piece_uploads` for observability.
  - **`task_prove.go` RetryWait** linear 30/60/90/120 across MaxFailures=5 (~5 min total budget). Previously fired retries back-to-back.
  - **Schema 0016**: SQLite table-rebuild adds FK `ON DELETE SET NULL` on `pdp_piece_uploads.notify_task_id` + `pdp_piecerefs.needs_indexing` + `.indexing_task_id`.
  Live verification: 0 ERROR / 0 WARN in 60s of runtime post-deploy; sweeper correctly does NOT match the healthy dataset 13977.
- ~~**[#58](https://github.com/Reiers/curio-core/issues/58)**~~ (closed 16:42 CEST). Audit conclusion: original framing was wrong (`SendTaskETH.Do` never returns error on send failure, so no harmonytask retry, so no double-send). Real race: DB UPDATE after successful broadcast can fail, triggering harmonytask retry which re-broadcasts the same nonce -> false-negative pattern (tx lands on-chain, app sees 'nonce too low' error, client gets HTTP 500 on a request that actually succeeded). Severity medium. Fix tracker filed as [#61](https://github.com/Reiers/curio-core/issues/61): pre-flight `TransactionByHash` + benign-error classification, ~30 LoC, both dependencies (EthClient.TransactionByHash, Lantern VMBridge forwarding) already in place.
- ~~**[#59](https://github.com/Reiers/curio-core/issues/59)**~~ (closed 16:05 CEST). Three SQLite portability bugs surfaced live during the P5 push were all fixed:
  - `pdp/indexing.go` `EnableIndexingForPiecesInTx` - `ANY($2)` with `[]int64` rewritten to IN-list (Reiers/curio @249dd68)
  - `pdp/handlers_pull.go` `getPiecesStatusBatch` - `ANY($1)` with `[]string` rewritten to IN-list (same commit)
  - `pdp_piece_pulls` schema rewritten to faithfully match upstream `20260109-pdp-v0-pull.sql` (curio-core @9e3a1b3); applied to live state.sqlite via direct DROP+CREATE.
  Daemon now logs 0 errors in 60s of runtime; previously each ~30s tipset cycle logged at least one of these three.

## Closed today (17 issues)

[#4](https://github.com/Reiers/curio-core/issues/4) (strategy decided), [#7](https://github.com/Reiers/curio-core/issues/7), [#8](https://github.com/Reiers/curio-core/issues/8), [#13](https://github.com/Reiers/curio-core/issues/13), [#16](https://github.com/Reiers/curio-core/issues/16), [#17](https://github.com/Reiers/curio-core/issues/17), [#29](https://github.com/Reiers/curio-core/issues/29) (audit), [#30](https://github.com/Reiers/curio-core/issues/30) (audit), [#31](https://github.com/Reiers/curio-core/issues/31) (audit), [#38](https://github.com/Reiers/curio-core/issues/38), [#46](https://github.com/Reiers/curio-core/issues/46), [#54](https://github.com/Reiers/curio-core/issues/54), [#55](https://github.com/Reiers/curio-core/issues/55), [#57](https://github.com/Reiers/curio-core/issues/57), [#58](https://github.com/Reiers/curio-core/issues/58) (audit), [#59](https://github.com/Reiers/curio-core/issues/59), [#61](https://github.com/Reiers/curio-core/issues/61).

## Product vision filed (#60)

Nicklas flagged at 16:08 CEST: Curio Core should be a turnkey Hot Storage product for laptop / regular-desktop deployments. Anyone can install, pick provider or client role, do guided setup, connect Brave/MetaMask wallet, run hot storage end-to-end. Monetization via small client-side commission on payment rails + optional premium UI subscription. Filed as [#60](https://github.com/Reiers/curio-core/issues/60) so the product shape stays anchored. Technical workstreams (`#40` wallet, `#36` retrieval gateway, `#37` settlement, `#39` SP dashboard, `#42` IPNI, `#52` client CLI) all align with this vision; the new ones (single-binary installer, WalletConnect, monetization layer, premium UI gating) will be filed when we pivot to the V0.1 turnkey release after #56 P5 closes.

## Files of record

| File | What it holds |
|---|---|
| `README.md` | Public-facing overview + Hot Storage FoC product spec |
| `docs/PLAN.md` | Day-by-day plan with acceptance criteria |
| `docs/STATUS.md` | This file - live status |
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

- Don't track `internal/harmonysqlite/migrations/` - that path is gitignored because background tooling auto-syncs it from the upstream Curio module cache, which would clobber the curio-core canonical schema at `schema-curio-core/`.
- `cmd/inspect-tables/` is a dev tool: dumps the SQLite schema produced by `harmonysqlite.New()` to stdout. Not shipped in release builds.
- `internal/curiowire/` is scratch space used during the Day 2 + 4 wire-up to surface Curio compilation gaps. May be deleted once the bundle stabilizes.
- `scripts/check-footprint` enforces a 90 MB hard limit on the curio-core binary in CI (#53).
- `admin/test-tx` is the canonical "is the on-chain sender pipeline alive" endpoint; first successful invocation landed in calibration block 3742933.
