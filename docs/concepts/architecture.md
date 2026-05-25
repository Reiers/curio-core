# Architecture

Curio Core is a **slim fork** of upstream [filecoin-project/curio](https://github.com/filecoin-project/curio)
that replaces:

| Upstream | Curio Core | Why |
|---|---|---|
| 3-node Yugabyte cluster | Single SQLite file | One-binary deployment; SQLite handles single-node load easily |
| Lotus full node sidecar | Embedded Lantern light client | No 76 GB chain DB; full DRAND + F3 finality verification in-process |
| CGo + Rust + filecoin-ffi | Pure Go (`CGO_ENABLED=0`) | Single static binary, cross-compile in seconds |
| Yugabyte's `harmonydb` driver | `harmonyquery.DBInterface` seam | Same harmonytask scheduler, swap backend transparently |
| External eth RPC | In-process FEVM bridge | One process owns the entire stack |
| Curio cluster, Boost market, IPNI sidecar... | All folded into the daemon | Operator runs `curio-core run` and is done |

The result is a **~88 MB static binary** that holds a complete Filecoin hot-storage SP.

## Process layout

```
┌──────────────────────────────────────────────────────────────────┐
│  curio-core run                                                   │
│                                                                   │
│  ┌────────────────┐   ┌──────────────────┐   ┌──────────────────┐ │
│  │ Embedded       │   │ harmonytask      │   │ HTTP server      │ │
│  │ Lantern        │   │ scheduler        │   │ (chi.Mux)        │ │
│  │ (chain sync)   │   │                  │   │                  │ │
│  └───────┬────────┘   │  ┌────────────┐  │   │ /pdp/*           │ │
│          │            │  │ PDPv0_*    │  │   │ /piece/*         │ │
│  ┌───────▼────────┐   │  │ Prove/InitPP│ │   │ /admin/*         │ │
│  │ Headerstore    │   │  │ PullPiece   │ │   │ /                │ │
│  │ + DRAND beacons│   │  │ PaySettle   │ │   │ (dashboard)      │ │
│  └────────────────┘   │  │ ParkComplete│ │   └────────┬─────────┘ │
│                       │  └────────────┘  │            │           │
│  ┌────────────────┐   │                  │            │           │
│  │ FEVM Bridge    │◄──┤  ┌────────────┐  │            │           │
│  │ (eth_*)        │   │  │ SendTaskETH│  │            │           │
│  └────────────────┘   │  └────────────┘  │            │           │
│                       └────────┬─────────┘            │           │
│                                │                      │           │
│              ┌─────────────────▼──────────────────────▼─────────┐ │
│              │  SQLite state DB (one .sqlite file)              │ │
│              │  - harmony_task, harmony_task_history            │ │
│              │  - pdp_data_sets, pdp_payment_rails, parked_*    │ │
│              │  - eth_keys, message_sends_eth, harmony_config   │ │
│              │  - alerts, message_waits_eth                     │ │
│              └─────────────────────┬────────────────────────────┘ │
│                                    │                              │
└────────────────────────────────────┼──────────────────────────────┘
                                     │
                                     ▼
                              <data-dir>/stash/*
                              (client piece bytes,
                               1:1 with raw size)
```

Everything inside the box is in one process. The only external dependencies are:

- the Filecoin chain itself (via Lantern's libp2p peers + the FEVM bridge upstream)
- the local filesystem (state DB + piece bytes)
- (optionally) an nginx reverse proxy in front for TLS + path filtering

## Key seams

The fork rests on a few small interface extractions in upstream code:

- **`harmonyquery.DBInterface`** — new 4-method interface (`ExecI`, `SelectI`, `QueryRowI`,
  `BeginTransactionI`) implemented by both `*harmonydb.DB` (Yugabyte/pgx) and our
  SQLite driver. The harmonytask scheduler talks to the interface, not the concrete type.

- **`pieceprovider.PieceParkBackend`** — interface that lets `localpiecepark.Reader`
  satisfy the upstream piece-bytes-resolver contract without dragging in the cluster-
  aware `paths.Remote` + `paths.SectorIndex` storage abstraction.

- **`diskstash.StashStore`** — implements the upstream `paths.StashStore` used by the
  streaming-upload path, but writes to local files instead of the cluster bytes ladder.

Together these mean Curio Core consumes upstream Curio **as a Go module**, with no
patched-binary drift to maintain.

## What's missing

Curio Core deliberately does **not** include:

- Sealing (`SDR`, `TreeRC`, `PoRep`, `CommitBatch`, etc.) — hot-storage doesn't seal.
- WindowPoSt / WinningPoSt — those are sealed-storage proof tasks.
- IPFS HTTP gateway — clients can build one in front if they need IPLD verifiable
  streaming; the SP serves raw piece bytes.
- IPNI announcer — separate concern, currently not wired (tracking in #42).

If you need any of those, run upstream Curio.

## Code map

| Path | Purpose |
|---|---|
| `cmd/curio-core/` | CLI entrypoints: `run`, `wallet`, `demo`, `sp`, `doctor`, `version` |
| `internal/dashboard/` | The operator + client WebUI |
| `internal/payments/` | USDFC rail discovery + `settleRail` singleton task |
| `internal/parkcomplete/` | Streaming-upload completion bridge |
| `internal/localpiecepark/` | Local piece-byte reader implementing `PieceParkBackend` |
| `internal/diskstash/` | Local-disk `StashStore` implementation |
| `internal/retrieval/` | `GET /piece/{cid}` HTTP read path |
| `internal/admin/` | `/admin/test-tx` + `/admin/eth-key` + alerts |
| `internal/setupweb/` | `/setup` first-run wizard |
| `internal/harmonysqlite/` | SQLite implementation of `harmonyquery.DBInterface` |
| `internal/engine/` | harmonytask engine + TaskRegistry |
| `internal/ethkeys/`, `internal/wallet/` | secp256k1 key management |
| `internal/pdpwire/` | Wires upstream `pdp.PDPService` into curio-core's deps |
