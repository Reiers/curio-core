<div align="center">

<img src="docs/assets/curio-core-mark-256.png" alt="Curio Core" width="120" />

# Curio Core

**A complete Filecoin Onchain Cloud hot-storage provider in a single binary, under 1 GB.**

*Pure Go. Embedded chain node. SQLite, not Yugabyte. No Lotus sidecar.*

[![License: Apache 2.0 OR MIT](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue.svg)](#license)
[![Beta](https://img.shields.io/badge/status-beta-22BFC4.svg)](#status)
[![Release](https://img.shields.io/badge/release-v0.1.0--beta.1-22BFC4)](https://github.com/Reiers/curio-core/releases/tag/v0.1.0-beta.1)
[![Docs](https://img.shields.io/badge/docs-curio--core--docs.pages.dev-22BFC4)](https://curio-core-docs.pages.dev/)

Website: **[curiocore.io](https://curiocore.io)** · Chain backend: **[golantern.io](https://golantern.io)**

</div>

---

## TL;DR

Curio Core is a Filecoin Onchain Cloud **hot-storage SP, in one static Go binary**. PDP task pipeline, payments, IPNI, operator+client WebUI, and an embedded Lantern chain node, all in a single ~90 MB process. **CGO_ENABLED=0**. No `filecoin-ffi`. No Rust toolchain. No Yugabyte cluster. No Lotus sidecar. No external eth RPC.

**Beta, proven end-to-end on Filecoin mainnet.** The full hot-storage flow now runs on mainnet from a single machine (an old Mac mini, native arm64): SP registration → self-funded USDFC → payments → dataset creation → addPieces → live proving cycle. Every tx signed by the binary itself, zero Glif, no Lotus.

| | |
|---|---|
| Latest release | **[v0.1.0-beta.1](https://github.com/Reiers/curio-core/releases/tag/v0.1.0-beta.1)** |
| Mainnet provider ID | **31** (self-registered, `oldlaptop.reiers.io`) |
| First mainnet dataset | **#1311** (`createDataSet` status `0x1`) |
| First mainnet addPieces | `0x6311d186…` status `0x1`, block **6,124,899** |
| Mainnet proving cycle | **live** (dataset 1311, `prove_at_epoch` 6,127,755) |
| SP host | one Mac mini, native arm64, CGO-free |
| Calibration soak (overnight 2026-05-25) | **8 / 8** prove cycles, **5** USDFC settles |
| Binary size (linux/amd64, no CGo) | **~90 MB** |
| Process RSS at idle | **~55 MB** |
| SQLite state.sqlite | **~2 MB** |

> **Status:** beta (`v0.1.0-beta.1`). First full mainnet PDP e2e is done; mainnet is now supported, with hardening (auth layer, operator runbook, soak) ongoing toward GA. Live tracking: [Curio Core Status Overview](https://github.com/Reiers/curio-core/issues/10). Docs: <https://curio-core-docs.pages.dev/>.

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
| **Lantern** | `Reiers/lantern` (in-process library) | Lotus-compatible JSON-RPC backend. Chain head, state reads, eth_*, signing, JWT auth. Bounded VMBridge fallback for FEVM execution. |
| **PDP HTTP API** | `Reiers/curio` `pdp/` (PDP-only fork) | Full synapse-sdk wire surface: piece uploads (streaming), data-set creation, addPieces, terminate, retrieval. |
| **harmonytask scheduler** | same fork | PDP task lifecycle: proof generation, NextPP, NotifyTask, SendTransaction, PullPiece. |
| **harmonysqlite** | this repo `internal/harmonysqlite` | SQLite-backed `harmonyquery.DBInterface` implementation. Replaces Yugabyte/Postgres without changing upstream code. |
| **payments** | this repo `internal/payments` | Singleton harmonytask `PDPv0_PaySettle`: USDFC rail discovery via `FilecoinPay.getRailsForPayeeAndToken` + `settleRail` dispatch every 10 minutes. |
| **retrieval** | this repo `internal/retrieval` | `GET /piece/{cid}` HTTP read path with HTTP Range, ETag, immutable cache. Reads from `parked_pieces` + `parked_piece_refs` via `localpiecepark`. |
| **dashboard** | this repo `internal/dashboard` | Operator + client WebUI: chain head, datasets, USDFC rails, scheduler health, wallets, storage, upload, embedded terminal. Dark-mode, server-rendered, zero JS framework. |
| **diskstash** | this repo `internal/diskstash` | Local-disk `paths.StashStore` implementation for the piece upload pipeline. |
| **localpiecepark** | this repo `internal/localpiecepark` | Local piece-byte reader implementing `pieceprovider.PieceParkBackend`. |
| **parkcomplete** | this repo `internal/parkcomplete` | Bridge task that flips `parked_pieces.complete=1` when streaming-upload bytes land. |
| **nodeapi + ethclient** | this repo `internal/nodeapi`, `internal/ethclient` | Dial embedded Lantern over `/rpc/v1` with self-minted admin JWT. Standard Lotus + go-ethereum client surfaces, in-process. |
| **ethkeys + wallet** | this repo `internal/ethkeys`, `internal/wallet` | Auto-generate or import a calibration/mainnet wallet at boot. Persist in SQLite. Full operator CLI: list, new, import, export, role, delete, send (FIL + USDFC). |
| **admin endpoints** | this repo `internal/admin` | `/admin/test-tx`, `/admin/eth-key`, `/admin/alerts/*` — loopback-only operator hooks. |
| **setupweb** | this repo `internal/setupweb` | `/setup` first-run wizard for the three required SP identifiers. |

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
| **Full mainnet PDP e2e** (register → fund → dataset → addPieces → prove) | ✅ **proven on mainnet** (v0.1.0-beta.1, dataset #1311) |
| Headless USDFC self-funding (`wallet get-usdfc`, Squid + SushiSwap V3) | ✅ shipped ([#92](https://github.com/Reiers/curio-core/issues/92)) |
| USDFC readiness preflight in `doctor` | ✅ shipped ([#91](https://github.com/Reiers/curio-core/issues/91)) |
| Embedded Lantern (calibration + mainnet) | ✅ shipped (v1.7.21, zero-Glif read+write path) |
| `/pdp/piece/uploads` streaming pipeline | ✅ live, end-to-end on cc-smoke |
| SQLite-backed harmonytask scheduler | ✅ shipped (`harmonyquery.DBInterface` seam) |
| Real on-chain tx via embedded Lantern | ✅ shipped (8 successful prove cycles overnight 2026-05-25) |
| PDPService → SenderETH harmonytask broadcast | ✅ shipped |
| PDPv0 PullPiece refactor (upstream PR #1245) | ✅ adopted ([#24](https://github.com/Reiers/curio-core/issues/24)) |
| Auto-generated `eth_keys` wallet | ✅ shipped |
| VMBridge for FEVM forwarding | ✅ shipped (calibration/mainnet Glif defaults) |
| Wallet management CLI (list/new/import/export/role/delete/send) | ✅ shipped — FIL + USDFC sends verified live ([#40](https://github.com/Reiers/curio-core/issues/40)) |
| Doctor CLI (DB ↔ on-chain reconciliation, observe-only) | ✅ shipped ([#41](https://github.com/Reiers/curio-core/issues/41)) |
| SP Registry CLI (`sp info` + `sp register`) | ✅ shipped |
| PDPVerifier v3.4.0 ABI + 0.1 FIL cleanup deposit handling | ✅ shipped ([#63](https://github.com/Reiers/curio-core/issues/63)) |
| HTTP retrieval (`/piece/{cid}`) | ✅ shipped ([#36](https://github.com/Reiers/curio-core/issues/36)) — Range, ETag, 1.2 GB/s aggregate |
| USDFC payment receiver + rail settlement | ✅ shipped ([#37](https://github.com/Reiers/curio-core/issues/37)) — 5 on-chain settles confirmed |
| Operator dashboard (premium WebUI) | ✅ first cut shipped ([#39](https://github.com/Reiers/curio-core/issues/39)) — iterating |
| Documentation site | ✅ live at [curio-core-docs.pages.dev](https://curio-core-docs.pages.dev/) ([#66](https://github.com/Reiers/curio-core/issues/66)) |
| IPNI provider | ⏳ [#42](https://github.com/Reiers/curio-core/issues/42) |
| Session Key Registry | ⏳ [#44](https://github.com/Reiers/curio-core/issues/44) |
| synapse-sdk compat test suite | ⏳ [#46](https://github.com/Reiers/curio-core/issues/46) |
| Client-side CLI (drive any SP) | ⏳ [#52](https://github.com/Reiers/curio-core/issues/52) |

Full open roadmap: [issues by label](https://github.com/Reiers/curio-core/issues).

## Try it

Grab a release binary/package from [Releases](https://github.com/Reiers/curio-core/releases) (deb/rpm/pkg/raw for linux amd64+arm64, macOS arm64), or build from source:

```sh
# build
git clone https://github.com/Reiers/curio-core
cd curio-core
CGO_ENABLED=0 go build -o curio-core ./cmd/curio-core

# probe (sanity check the embedded Lantern)
./curio-core probe --network calibration --timeout 30s

# run (full daemon)
./curio-core run --network calibration --data-dir ~/.curio-core --listen 127.0.0.1:4711
```

### Operator CLIs (no daemon needed)

```sh
# Wallet management
curio-core wallet list
curio-core wallet new --role backup
curio-core wallet import --role pdp <0xhex-private-key>
curio-core wallet export --confirm <0xaddr>
curio-core wallet role <0xaddr> backup
curio-core wallet delete --yes <0xaddr>

# Self-fund USDFC (bridge USDC from an L1/L2 -> USDFC on Filecoin, no browser)
curio-core wallet get-usdfc --amount 3 --from-chain base          # quote only
curio-core wallet get-usdfc --amount 3 --from-chain base --submit  # execute

# Health + reconciliation report (read-only)
curio-core doctor --network calibration

# SP Registry operations
curio-core sp info
curio-core sp register --name "Acme PDP" --description "Hot storage SP" --dry-run
```

Boot log:
```
curio-core run: starting daemon
  data-dir: /home/op/.curio-core
  network:  calibration
  db-path:  /home/op/.curio-core/state.sqlite
  listen:   127.0.0.1:4711
  lantern:  anchored at epoch 3745971
  lantern:  rpc at http://127.0.0.1:41763/rpc/v1 (in-process)
  lantern:  vm-bridge -> https://api.calibration.node.glif.io/rpc/v1
  eth_keys: 0xf73Aa7b26Cd1fd30A7D5039842E13A8C7344CfEe (role=pdp)
  payments: USDFC rail settler active (every 10m0s, payee=0xf73Aa7b26...)
  engine:   10 live task impls, 11 descriptor entries
  watchers: pdpv0 dataset/terminate/delete handlers wired on tipset sub
  parkcomplete: streaming-upload -> parked_pieces.complete bridge active
  alerts:   /admin/alerts active (task-history poller, 30s interval)
  pdp:      /pdp/* routes mounted (stash /home/op/.curio-core/stash)
  admin:    /admin/test-tx, /admin/eth-key mounted (loopback)
  retrieval:/piece/{pieceCid} mounted (HTTP Range, ETag, immutable cache)
  dashboard:/, /wallets, /datasets, /rails, /tasks, /alerts mounted (Curio Core branded)
```

### Operator dashboard

A full operator dashboard ships in the binary at `http://127.0.0.1:4711/`:

- **Overview** — chain head, dataset count, pieces stored, active rails, 24h proof stats, scheduler health
- **Wallets** — live tFIL + USDFC balances, FIL/USDFC send form
- **Datasets** — active client storage with proof status
- **Rails (USDFC)** — per-rail payment rate, total incoming USDFC/epoch, last `settleRail` tx
- **Tasks** — active queue + last 50 history rows
- **Storage** — piece count, logical bytes, physical stash-dir disk usage
- **Upload** — client-facing 2-phase streaming upload with XHR progress bar
- **Terminal** — allowlisted curio-core CLI runner (`version`, `wallet list`, `doctor`, `sp info`, `probe`, `config show`)

Loopback-only by design. Access via SSH tunnel: `ssh -L 4711:127.0.0.1:4711 your-sp-host`.

See the [dashboard tour](https://curio-core-docs.pages.dev/operating/dashboard) in the docs for the full walkthrough.

### Upload a piece

```sh
# 1. POST /pdp/piece/uploads -> 201 + Location header
LOC=$(curl -sX POST https://sp.example.com/pdp/piece/uploads -D - | awk -F': ' 'tolower($1)=="location"{gsub(/[\r\n]/,"");print $2}')

# 2. PUT bytes -> 204; server computes PieceCID v1, stores to disk + SQLite
curl -X PUT --data-binary @file.bin https://sp.example.com$LOC
```

### Get the SP's wallet

```sh
curl http://127.0.0.1:4711/admin/eth-key
# {"address":"0x6b4758...833c","role":"pdp"}
```

### Trigger a test on-chain transaction

```sh
curl -X POST http://127.0.0.1:4711/admin/test-tx -d '{}'
# {"txHash":"0x4f350...","from":"0x6b4758...833c"}
```

(See [Day 8 milestone](https://github.com/Reiers/curio-core/issues/16) for the on-chain receipt.)

## Roadmap

**Phase 0 — Mainnet beta (done, 2026-06-21)**
- ✅ First full PDP hot-storage e2e on Filecoin **mainnet**, from a single Mac mini
- ✅ SP self-registration on mainnet (provider 31)
- ✅ Self-funded USDFC: FIL → WFIL → USDFC via SushiSwap V3, signed by the binary ([#92](https://github.com/Reiers/curio-core/issues/92))
- ✅ `createDataSet` → `addPieces` → live proving cycle on mainnet (dataset #1311)
- ✅ Tagged [v0.1.0-beta.1](https://github.com/Reiers/curio-core/releases/tag/v0.1.0-beta.1)

**Phase 1 — Foundations (done)**
- ✅ Lantern V1 minimal node (~40 MB, pure Go, calibration + mainnet)
- ✅ Pdpv0-only fork of upstream Curio
- ✅ Full SQLite port of the harmonytask + PDP schema
- ✅ Embedded Lantern with self-minted JWT, /rpc/v1 in-process
- ✅ Upload pipeline: client → curio-core → disk + SQLite, byte-identical
- ✅ Sign + broadcast on-chain via embedded Lantern, real receipts

**Phase 2 — Hot Storage SP product (done)**
- ✅ HTTP retrieval gateway ([#36](https://github.com/Reiers/curio-core/issues/36))
- ✅ FilecoinPay rail settlement ([#37](https://github.com/Reiers/curio-core/issues/37))
- ✅ SP Registry registration
- ✅ Operator dashboard, first cut ([#39](https://github.com/Reiers/curio-core/issues/39))
- ✅ Wallet management with FIL + USDFC send ([#40](https://github.com/Reiers/curio-core/issues/40))
- ✅ Doctor reconciliation ([#41](https://github.com/Reiers/curio-core/issues/41))
- ✅ Upstream PR #1245 PullPiece refactor adopted ([#24](https://github.com/Reiers/curio-core/issues/24))
- ✅ PDPVerifier v3.4.0 cleanup-deposit handling ([#63](https://github.com/Reiers/curio-core/issues/63))
- ✅ Documentation site ([#66](https://github.com/Reiers/curio-core/issues/66))

**Phase 3 — Polish (in progress)**
- ⏳ Dashboard iteration: USDFC sends in-browser, wallet new/import flows, low-balance alerts
- ⏳ IPNI announcer ([#42](https://github.com/Reiers/curio-core/issues/42))
- ⏳ synapse-sdk compat test suite ([#46](https://github.com/Reiers/curio-core/issues/46))
- ⏳ Multi-piece delete ([#43](https://github.com/Reiers/curio-core/issues/43))
- ⏳ Session Key Registry ([#44](https://github.com/Reiers/curio-core/issues/44))
- ⏳ Per-operation fee structure ([#45](https://github.com/Reiers/curio-core/issues/45))
- ⏳ Aggregate root retrieval ([#49](https://github.com/Reiers/curio-core/issues/49))
- ⏳ Client-side CLI shipped in same binary ([#52](https://github.com/Reiers/curio-core/issues/52))

**Phase 4 — GA hardening (Q3 2026)**
- Mainnet bootstrap quorum for Lantern
- Production auth layer for the dashboard (today: loopback only)
- Operator runbook for the first paid client
- Live `pdp_data_sets` row-state drift fix ([#65](https://github.com/Reiers/curio-core/issues/65))
- Indexing-state observability for clients ([#93](https://github.com/Reiers/curio-core/issues/93))
- Mainnet soak across multiple proving windows
- TBD based on operator feedback.

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

## Advisors

Curio Core's scope, task carve-out, and overall architecture have benefited from technical advisory from the [Curio](https://github.com/filecoin-project/curio) core team:

- **[LexLuthr](https://github.com/LexLuthr)** — Curio core team. Reviewed Lantern's chain-node architecture ([Lantern#10](https://github.com/Reiers/lantern/issues/10)), which informed Curio Core's embedded-chain design.
- **[Andrew Jackson / @snadrus](https://github.com/snadrus)** — Curio core team. Bundle-architecture design ([Lantern#11](https://github.com/Reiers/lantern/issues/11)), SQLite-portable DB-seam approach, docs review ([#66](https://github.com/Reiers/curio-core/issues/66)).

Advisor roles are non-binding; views and code in this repository are the author's responsibility.

---

## License

Apache 2.0 OR MIT, contributor's choice. Same as Lantern.
