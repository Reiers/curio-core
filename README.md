<div align="center">

<img src="docs/assets/curio-core-mark-256.png" alt="Curio Core" width="120" />

# CURiO core

**A complete Filecoin Onchain Cloud hot-storage provider in a single binary, under 1 GB.**

*Pure Go. Embedded chain node. SQLite, not Yugabyte. No Lotus sidecar.*

[![License: Apache 2.0 OR MIT](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue.svg)](#license)
[![Pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange.svg)](#status)

</div>

---

> **Status: pre-alpha.** Built end-to-end (uploads + on-chain signing) on Filecoin Calibration. Mainnet readiness is a Q3 milestone. Tracking: [Curio Core Status Overview](https://github.com/Reiers/curio-core/issues/10).

## What this is

Curio Core is a **hot-storage Filecoin Onchain Cloud storage provider** that runs in one static binary.

It's the answer to "what's the minimum infrastructure I need to run a paid PDP storage business?"

Today that answer is roughly:
- a 76 GB Lotus full node
- a 3-node Yugabyte cluster
- a Curio cluster
- a Boost market node
- a public eth RPC sidecar for FEVM forwarding
- a separate IPNI announcer
- a dashboard, a wallet manager, a settlement watcher, monitoring...

Curio Core's answer is: **one binary**. Drop it on a single VM, point a domain at it, fund a wallet. You're a Filecoin Onchain Cloud hot-storage provider.

## The pitch in one image

```
┌──────────────────────────────────────────────────────────────────┐
│  curio-core (one static binary, ~90 MB, pure Go)                 │
│                                                                  │
│  ┌────────────────────┐    ┌────────────────────────────────┐    │
│  │  Lantern           │    │  PDP + FoC                     │    │
│  │  (embedded)        │    │                                │    │
│  │                    │    │  • upload   (POST /pdp/...)    │    │
│  │  • chain head      │◄──►│  • download (GET  /piece/...)  │    │
│  │  • state reads     │    │  • addPieces -> on-chain       │    │
│  │  • eth_*           │    │  • proof submission            │    │
│  │  • signing         │    │  • payment rail settlement     │    │
│  │  • JWT auth        │    │  • IPNI announce               │    │
│  │  • VMBridge        │    │  • SP registry registration    │    │
│  └────────────────────┘    │  • client tooling              │    │
│                            └────────────────────────────────┘    │
│                                                                  │
│  ┌────────────────────────────────────────────────────────┐      │
│  │  WebUI (operator dashboard, premium)                   │      │
│  │  Wallet • Datasets • Rails • Proofs • Alerts • Chain   │      │
│  └────────────────────────────────────────────────────────┘      │
│                                                                  │
│  Storage: SQLite (modernc.org/sqlite) + local disk piece stash   │
└──────────────────────────────────────────────────────────────────┘
                                ▲
                                │  HTTPS (synapse-sdk wire format)
                                │  /pdp/* + /piece/*
                                ▼
                ┌──────────────────────────────┐
                │  client                      │
                │  (synapse-sdk, viem, web)    │
                └──────────────────────────────┘
```

The Lantern half talks to the calibration/mainnet network for chain reads, message broadcast, and gas estimation. The PDP half does everything an SP operator actually needs.

## What's bundled

| Component | Source | Role |
|---|---|---|
| **Lantern** | `Reiers/lantern` (in-process library) | Lotus-compatible JSON-RPC backend. Chain head, state reads, eth_*, signing, JWT auth. Bounded VMBridge fallback for FEVM execution today. |
| **PDP HTTP API** | `Reiers/curio` `pdp/` (PDP-only fork) | Full synapse-sdk wire surface: piece uploads (streaming + CommP-first), data-set creation, addPieces, terminate, retrieval. |
| **harmonytask scheduler** | same fork | PDP task lifecycle: proof generation, NextPP, NotifyTask, SendTransaction, PullPiece, settlement watcher. |
| **harmonysqlite** | this repo `internal/harmonysqlite` | SQLite-backed `harmonyquery.DBInterface` implementation. Replaces Yugabyte/Postgres without changing upstream code. |
| **diskstash** | this repo `internal/diskstash` | Local-disk `paths.StashStore` implementation for the piece upload pipeline. |
| **nodeapi + ethclient** | this repo `internal/nodeapi`, `internal/ethclient` | Dial embedded Lantern over `/rpc/v1` with self-minted admin JWT. Standard Lotus + go-ethereum client surfaces, in-process. |
| **ethkeys** | this repo `internal/ethkeys` | Auto-generate or import a calibration/mainnet wallet at boot. Persist in SQLite. |
| **WebUI** | upstream curio `web/` (pdpv0-stripped) | Operator dashboard. Real-time SP state, payments, datasets, alerts. (Day 9+.) |
| **admin endpoints** | this repo `internal/admin` | `/admin/test-tx`, `/admin/eth-key`, etc. — loopback-only operator hooks. |

## Hot storage, not cold

**Curio Core is intentionally hot-storage only.**

| In scope (what we ship) | Out of scope (forever) |
|---|---|
| pdpv0 task pipeline | sealing (PoRep, SDR, syscalls to filecoin-ffi) |
| HTTP piece upload + download | WindowPoSt, WinningPoSt |
| FEVM tx signing + broadcast | mk12 / Boost market deals |
| FilecoinPay rail settlement | Yugabyte / Postgres / clustering |
| USDFC payments | multi-node failover |
| IPNI content routing | a separate full Lotus node |

Cold storage SPs run the full Curio + Lotus + sealing stack. That's a different shape of business with different infrastructure requirements. Curio Core is the answer for operators who want **paid PDP hot storage with payments and dashboards, today, on a single VM**.

## Design rules

1. **Pure Go.** Zero CGo, no Rust toolchain, no `filecoin-ffi`. `CGO_ENABLED=0 GOOS=linux go build` is the canonical build.
2. **Single binary, under 1 GB total footprint.** Binary ~90 MB. SQLite + piece-stash + Badger header store < 1 GB at scale. See [#14 footprint budget](https://github.com/Reiers/curio-core/issues/14).
3. **Single-server.** SQLite. No clustering, no peer coordination. The PDP-only operator profile is exactly the shape SQLite handles best.
4. **Lantern stays minimal.** Lantern is a general-purpose Filecoin light node and stays under 40 MB. The dependency direction is always `curio-core → lantern`, never the reverse.
5. **synapse-sdk wire compatibility.** Any synapse-sdk client should drive curio-core out of the box. Adopt upstream PDP API drift quickly.
6. **Self-contained.** Wallet, SP registration, payment settlement, dashboard, alerts — all in one binary. The operator can run a paid SP business without a separate Lotus, separate eth RPC, or separate payments dashboard.

## How it works (the chain trip)

```
client uploads a file via synapse-sdk
  -> HTTPS to https://sp.example.com/pdp/piece/uploads
    -> nginx -> curio-core listener on 127.0.0.1
      -> upstream curio/pdp PDPService receives the bytes
        -> diskstash writes /var/lib/curio-core/stash/<uuid>.tmp
        -> SQLite rows in pdp_piece_streaming_uploads + parked_pieces + parked_piece_refs
        -> PieceCID v1 + v2 computed pure-Go server-side (no FFI)
      -> returns HTTP 204 to client

client calls POST /pdp/data-sets/create-and-add
  -> PDPService builds the addPieces calldata
    -> SenderETH constructs the FEVM transaction
      -> nonce via embedded Lantern's eth_getTransactionCount (VMBridge-forwarded)
      -> gas estimate via embedded Lantern's eth_estimateGas (VMBridge-forwarded)
      -> sign locally with the eth_keys private key
      -> broadcast via embedded Lantern's eth_sendRawTransaction (VMBridge-forwarded)
    -> chain tx lands; PDPNotifyTask schedules proof generation
  -> proof submission tasks fire on the next proving period
  -> FilecoinPay rails accrue USDFC against this dataset
  -> settlement watcher (harmonytask) periodically calls FilecoinPay.settleRail
  -> USDFC lands in the SP's wallet
```

## Status snapshot

| Capability | State |
|---|---|
| Embedded Lantern (calibration + mainnet) | ✅ shipped (V1.4) |
| `/pdp/piece/uploads` streaming + `/pdp/piece` CommP-first | ✅ streaming live; CommP-first ⏳ schema port |
| SQLite-backed harmonytask scheduler | ✅ shipped, with one [time-column scan blocker](https://github.com/Reiers/curio-core/issues/17) for the SendTaskETH path |
| Real on-chain tx via embedded Lantern | ✅ shipped (calibration block 3,741,096; viem path) |
| PDPService → SenderETH harmonytask broadcast | ⏳ blocked on #17 |
| Auto-generated `eth_keys` wallet | ✅ shipped |
| VMBridge for FEVM forwarding | ✅ shipped (calibration/mainnet Glif defaults) |
| HTTP retrieval (`/piece/{cid}`) | ⏳ [#36](https://github.com/Reiers/curio-core/issues/36) |
| USDFC payment receiver + rail settlement | ⏳ [#37](https://github.com/Reiers/curio-core/issues/37) |
| SP Registry registration | ⏳ [#38](https://github.com/Reiers/curio-core/issues/38) |
| Operator dashboard | ⏳ [#39](https://github.com/Reiers/curio-core/issues/39) |
| Wallet management CLI/UI | ⏳ [#40](https://github.com/Reiers/curio-core/issues/40) |
| Doctor (DB ↔ on-chain reconciliation) | ⏳ [#41](https://github.com/Reiers/curio-core/issues/41) |
| IPNI provider | ⏳ [#42](https://github.com/Reiers/curio-core/issues/42) |
| Session Key Registry | ⏳ [#44](https://github.com/Reiers/curio-core/issues/44) |
| synapse-sdk compat test suite | ⏳ [#46](https://github.com/Reiers/curio-core/issues/46) |
| Client-side CLI (drive any SP) | ⏳ [#52](https://github.com/Reiers/curio-core/issues/52) |

Full open roadmap: [issues by label](https://github.com/Reiers/curio-core/issues).

## Try it (today)

```sh
# build
git clone https://github.com/Reiers/curio-core
cd curio-core
CGO_ENABLED=0 go build -o curio-core ./cmd/curio-core

# probe (sanity check the embedded Lantern)
./curio-core probe --network calibration --timeout 30s

# run (full daemon)
./curio-core run --network calibration --data-dir ~/.curio-core --listen 127.0.0.1:14994
```

Boot log:
```
curio-core run: starting daemon
  data-dir: /home/op/.curio-core
  network:  calibration
  db-path:  /home/op/.curio-core/state.sqlite
  listen:   127.0.0.1:14994
  lantern:  anchored at epoch 3741128
  lantern:  rpc at http://127.0.0.1:39055/rpc/v1 (in-process)
  lantern:  vm-bridge -> https://api.calibration.node.glif.io/rpc/v1
  eth_keys: 0x6b4758baAcE34519F4977A30f6bEcd473249833c (role=pdp)
  engine:   9 task types registered
  pdp:      /pdp/* routes mounted (stash /home/op/.curio-core/stash)
  admin:    /admin/test-tx, /admin/eth-key mounted (loopback)
  webui:    http://127.0.0.1:14994/
```

### Upload a piece

```sh
# 1. POST /pdp/piece/uploads -> 201 + Location header
LOC=$(curl -sX POST https://sp.example.com/pdp/piece/uploads -D - | awk -F': ' 'tolower($1)=="location"{gsub(/[\r\n]/,"");print $2}')

# 2. PUT bytes -> 204; server computes PieceCID v1, stores to disk + SQLite
curl -X PUT --data-binary @file.bin https://sp.example.com$LOC
```

### Get the SP's wallet

```sh
curl http://127.0.0.1:14994/admin/eth-key
# {"address":"0x6b4758...833c","role":"pdp"}
```

### Trigger a test on-chain transaction

```sh
curl -X POST http://127.0.0.1:14994/admin/test-tx -d '{}'
# {"txHash":"0x4f350...","from":"0x6b4758...833c"}
```

(See [Day 8 milestone](https://github.com/Reiers/curio-core/issues/16) for the on-chain receipt.)

## Roadmap

**Phase 1 — Foundations (done)**
- ✅ Lantern V1 minimal node (40 MB, pure Go, calibration + mainnet)
- ✅ Pdpv0-only fork of upstream Curio
- ✅ Full SQLite port of the harmonytask + PDP schema
- ✅ Embedded Lantern with self-minted JWT, /rpc/v1 in-process
- ✅ Upload pipeline: client → curio-core → disk + SQLite, byte-identical
- ✅ Sign + broadcast on-chain via embedded Lantern, real receipt

**Phase 2 — Hot Storage SP product (in progress)**
- ⏳ HTTP retrieval gateway (#36)
- ⏳ FilecoinPay rail settlement (#37)
- ⏳ SP Registry registration (#38)
- ⏳ Operator dashboard (#39)
- ⏳ Wallet management (#40)
- ⏳ Doctor reconciliation (#41)
- ⏳ IPNI announcer (#42)
- ⏳ synapse-sdk compat test suite (#46)

**Phase 3 — Polish (after Phase 2 ships)**
- Multi-piece delete (#43)
- Session Key Registry (#44)
- Per-operation fee structure (#45)
- Adopt upstream alerts wiring (#48)
- Aggregate root retrieval (#49)
- Client-side CLI shipped in same binary (#52)

**Phase 4 — Beyond**
- TBD based on operator feedback. Probable directions: multi-wallet workflows, payment auction support, region-aware client steering.

## Differentiators

Curio Core competes with the full Curio + Lotus + Boost stack for the **PDP-only operator** profile. Its claims:

- **Single binary**. No Lotus, no Yugabyte, no Boost, no eth-rpc sidecar.
- **Under 1 GB total footprint** at steady state.
- **Pure Go**. Easy to deploy, easy to inspect, easy to fork.
- **synapse-sdk compatible** out of the box.
- **Operator-friendly**. Built-in dashboard, wallet management, doctor reconciliation, alerts.
- **Self-contained payments**. USDFC settlement is part of the binary, not a separate service.

What it does NOT do:

- Cold storage / sealing / PoRep / WindowPoSt. Use upstream Curio for that.
- Multi-node failover. Single-server by design.
- Permissionless block production. Lantern is a light node, not a miner.

## Relationship to upstream

Curio Core is built on top of forks:

- [`Reiers/curio`](https://github.com/Reiers/curio) — fork of `filecoin-project/curio`. Branch `db-seam-refactor` carries SQLite portability + DB-seam interface refactor that lets the upstream task scheduler run against a non-Postgres backend.
- [`Reiers/lotus`](https://github.com/Reiers/lotus) — fork of `filecoin-project/lotus`. Carries `storage/paths` carve-out so pdpv0 compiles under `CGO_ENABLED=0`.
- [`Reiers/harmonyquery`](https://github.com/Reiers/harmonyquery) — fork of `curiostorage/harmonyquery`. Adds `DBInterface` + `TxInterface` so the SQLite backend is pluggable.
- [`Reiers/lantern`](https://github.com/Reiers/lantern) — the chain-node half. Used as a library here, not vendored.

Upstream PRs and issues we're tracking for adoption: see the [adoption-labelled issues](https://github.com/Reiers/curio-core/issues?q=is%3Aissue+is%3Aopen+label%3Aadoption).

## Footprint discipline

Curio Core measures itself against a **hard 1 GB total disk + memory footprint at steady state with 1k pieces stored**. Tracked in [#14](https://github.com/Reiers/curio-core/issues/14).

Current breakdown (idle, no pieces):

| Component | Footprint |
|---|---|
| Binary (linux/amd64, no CGo) | ~90 MB |
| Process RSS (idle) | ~55 MB |
| SQLite state.sqlite | ~2 MB |
| Disk stash | empty |
| Badger header store (Lantern, future) | ~20-100 MB depending on uptime |
| **Total at idle** | **~170 MB** |

At scale with 1k pieces × 4 KB stub + active payment rails: well under 1 GB.

If a PR pushes the binary over 100 MB, that's a deliberate decision documented in the relevant issue.

## Brand

Mark and wordmark are derived from the parent Curio brand: the original is a layered isometric cube with a teal accent slit; Curio Core reduces to a single rhombus (the front face) with a teal dot at the geometric center (literally "the core"). Same teal accent `#22BFC4` carried through to the wordmark's lowercase `i`.

Assets live in [`docs/assets/`](docs/assets/).

## License

Apache 2.0 OR MIT, contributor's choice. Same as Lantern.
