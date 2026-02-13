# Alpha2.1 Checklist

## Done

- P0: Persistent blockstore introduced (`data/<network>/blockstore/*.blk`)
- P0: Persistent chainstore with canonical head (`data/<network>/chainstore/head.json`)
- P0: Snapshot import now materializes chain data into blockstore and commits head/state root
- P0: Sync vertical slice now advances head incrementally after startup (post-import continuity path)
- P0: `curiocore status` now reports real chain height from chainstore
- P0: `curiocore doctor --post-import` checks added:
  - chain head exists
  - blockstore populated
  - state root reachable
- P1 usability: Minimal RPC surface added:
  - `/rpc/v0/status`
  - `/rpc/v0/chain/head`
  - `/rpc/v0/chain/message/:cid`
  - `/metrics`
- Tests added for store, importer, and doctor post-import checks

## What’s next

- Replace incremental sync placeholder with real peer fetch + tipset/header validation pipeline
- Add bad block cache + fork choice + reorg-safe application path
- Add real message index for RPC message lookup
- Add auth model for RPC endpoints
- Add FVM apply path behind feature flag

## Blocked

- Full consensus/VM parity requires deeper Lotus-compatible validation execution integration
- High-throughput SP message handling depends on real mempool + network exchange path

## Complexity buckets (effort only)

- **S**
  - RPC auth middleware (local token mode)
  - richer metrics labels and counters
- **M**
  - bad block cache and fork-choice module
  - header validation pipeline with persisted checkpoints
- **L**
  - full state transition validation parity with Lotus/FVM
  - production-grade p2p sync and SP traffic handling under reorg stress

## How to verify

1. Initialize and run fast sync/import:
   - `curiocore init`
   - `curiocore sync --yes --mode fast`
2. Verify status contains height:
   - `curiocore status`
3. Verify post-import health:
   - `curiocore doctor --post-import`
4. Run RPC server:
   - `curiocore rpc serve --listen :1234`
5. Query endpoints:
   - `curl localhost:1234/rpc/v0/status`
   - `curl localhost:1234/rpc/v0/chain/head`
   - `curl localhost:1234/metrics`
