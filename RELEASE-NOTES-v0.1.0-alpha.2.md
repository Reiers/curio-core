## Curio Core v0.1.0-alpha.2

> [!WARNING]
> **ALPHA — calibration-only.** This build has only ever been exercised end-to-end on the **Filecoin calibration testnet**. It has **never run on mainnet**. Do not point it at a mainnet wallet, mainnet SP identity, or real funds. The on-chain write path, proving loop, and bridge-off Glif-free path are all proven on calibration but are not mainnet-hardened. Run it on calibration, with a throwaway wallet, and expect rough edges. Back up nothing you can't lose.

## ✨ Overview

Curio Core is a PDP-only Curio fused with an embedded [Lantern](https://github.com/Reiers/lantern) pure-Go Filecoin light client, shipped as a single CGO-free binary. No filecoin-ffi, no Rust, no separate Lotus, no Postgres/Yugabyte — SQLite for state, Lantern for chain. The goal is turnkey hot storage that runs on a laptop or a regular desktop.

`v0.1.0-alpha.2` is the **zero-Glif keystone** release. Since `alpha.1`, the entire read **and** write path to the chain has been moved off the external Glif RPC and onto the embedded Lantern: local FEVM `eth_call`, local nonce/gas/`sendRawTransaction`/receipt, and a Filecoin-native bitswap block source. It also gains native OS packages (.deb/.rpm/.pkg), first-class macOS support, and a batch of GA-critical reliability fixes found while soaking on calibration.

| Field | Value |
|---|---|
| Version | v0.1.0-alpha.2 |
| Type | **Alpha, calibration-only (not for mainnet)** |
| Compare | [v0.1.0-alpha.1...v0.1.0-alpha.2](https://github.com/Reiers/curio-core/compare/v0.1.0-alpha.1...v0.1.0-alpha.2) (45 commits) |
| Build | Go 1.26+ required to build from source; `CGO_ENABLED=0` |
| Chain backend | Embedded Lantern v1.7.16 (pure-Go light client) |
| Network | **Calibration testnet only** |
| Footprint | ~100 MB single binary |

## ⭐ Highlights

### 🔌 Zero-Glif read path — local FEVM `eth_call` ([#43](https://github.com/Reiers/lantern/issues/43), [#44](https://github.com/Reiers/lantern/issues/44))

Curio Core no longer needs an external Glif RPC to read contract state. Lantern now runs a pure-Go FEVM and serves `eth_call` locally against locally-validated chain state, with a head-driven prefetcher that warms PDPVerifier, FWSS, ServiceProviderRegistry and USDFC on every head advance, plus adaptive warming that learns new contracts on a miss. Proven on calibration: 100% of a 220-read sample served locally, zero bridge fallback.

### ✍️ Zero-Glif write path — local nonce, gas, send & receipt ([lantern #45](https://github.com/Reiers/lantern/issues/45))

The full transaction lifecycle is now local: `eth_getTransactionCount` (live-head-anchored), `eth_estimateGas` (conservative local heuristic), `eth_sendRawTransaction` (pure-Go EIP-1559 RLP codec + ecrecover → MpoolPush over gossipsub), and `eth_getTransactionReceipt` / `eth_getTransactionByHash` (local message-search). Verified bridge-off on calibration: a real signed tx submitted, landed, and its receipt resolved **byte-identical to Glif** with the external RPC disabled.

### 🧱 Filecoin-native bitswap block source ([lantern #50](https://github.com/Reiers/lantern/issues/50))

Block fetching for the chain path now uses libp2p Bitswap with the **Filecoin `/chain/ipfs/bitswap` protocol prefix** (boxo defaults to the IPFS prefix, which Filecoin peers don't speak — same lesson as the `/fil/kad` DHT prefix). Glif is eliminated from the block-fetch path on calibration.

### 🧾 v2-AMT receipt decoding ([lantern #49](https://github.com/Reiers/lantern/issues/49))

Block-message and ParentMessageReceipt AMTs are go-amt-ipld **v2** (3-field root, implicit width 8) while FEVM contract state is v4. Lantern now routes message/receipt search through a CGO-free v2 reader, fixing the "wrong number of fields" decode failures that previously blocked local receipts.

### 📦 Native OS packages + macOS support ([#68](https://github.com/Reiers/curio-core/issues/68), [#80](https://github.com/Reiers/curio-core/issues/80))

`curio-core` now ships as `.deb` / `.rpm` (linux amd64 + arm64) with a systemd unit, and as a `.pkg` for macOS arm64. macOS is a first-class build target: a drop-in `third_party/gosigar` replace makes the tree build CGO-free on darwin (no filecoin-ffi). An opt-in `curio-core upgrade` command checks GitHub releases for a newer version.

### 🌐 Two-port HTTP with baked-in TLS ([#69](https://github.com/Reiers/curio-core/issues/69))

The daemon now serves an admin port and a public port with built-in autocert TLS, removing nginx from the default deployment path. WebUI and CLI default to `:4711`.

### 🛠️ GA-critical reliability fixes (found soaking on calibration)

- **harmony_machines keepalive** ([#76](https://github.com/Reiers/curio-core/issues/76)/[#77](https://github.com/Reiers/curio-core/issues/77)) — new task dispatch used to silently die ~10 minutes after every boot on an FK constraint. Engine now keeps the machine row alive.
- **ETH message watcher ordering** ([#81](https://github.com/Reiers/curio-core/issues/81)) — the tx-confirmation poller was being registered after the scheduler started and got rejected, stalling PDP dataset/terminate/delete on `ok IS NULL`. Now wired via an `OnBeforeChainSched` hook.
- **SQLite seam closed** ([#73](https://github.com/Reiers/curio-core/issues/73)) — the deal pipeline runs end-to-end against SQLite.
- **HeadChange revert coalescing** ([#78](https://github.com/Reiers/curio-core/issues/78)) and **prove_at_epoch drift recovery** ([#79](https://github.com/Reiers/curio-core/issues/79)) — quieter logs and self-healing proving windows.
- **wake-at-write for PDPv0_Notify** ([#67](https://github.com/Reiers/curio-core/issues/67)) — faster pickup of completed uploads.

### 🔄 Upstream Curio catch-up

The `Reiers/curio` fork is caught up to upstream `filecoin-project/curio` main (27 commits): the post-#1245 PDP pull pipeline (per-item retry/backpressure with Retry-After), dataset-verify, dynamic FIL cleanup deposit, IP-offense throttle, and the #1293 IPNI throughput fix — all re-applied over the curio-core SQLite/harmonyquery seam, runtime-network-aware contract addresses, and SQLite portability. Piece-GC and the client-termination/cleanup feature set are intentionally kept disabled (they caused proving failures on a live SP).

## 📥 Install

**macOS (arm64):** download `curio-core-*-arm64.pkg` and `sudo installer -pkg <file> -target /`, or grab the raw `curio-core-darwin-arm64` binary.

**Linux:** `.deb` / `.rpm` for amd64 + arm64 (installs a systemd unit), or the raw `curio-core-linux-<arch>` binary.

**From source:** `CGO_ENABLED=0 go build ./cmd/curio-core` (Go 1.26+).

Quick start (calibration): `curio-core probe` to smoke-test the embedded Lantern, then `curio-core run`. See `docs/` for the guided setup, wallet, and SP-registration flow.

## ⚠️ Known limitations

- **Calibration only.** No mainnet end-to-end has been run. Mainnet is wired but unproven.
- **Bridge-off boot is flaky** — gossipsub head-tracking can stall >30s on calibration libp2p churn; while down, live-head reads hang. The Glif VMBridge is still available as a safety net and is the default.
- **#50 residual** — roughly one message block per write-confirm cycle still falls back on calibration sparsity; not yet unconditionally Glif-free under load.
- **Sent-tx index is in-memory** — lost on daemon restart (doesn't affect normal mid-flight runtime).
- Out of scope by design: sealing, PoRep, WindowPoSt, mk12/Boost, Yugabyte/Postgres, a separate Lotus.

**Full Changelog:** https://github.com/Reiers/curio-core/compare/v0.1.0-alpha.1...v0.1.0-alpha.2
