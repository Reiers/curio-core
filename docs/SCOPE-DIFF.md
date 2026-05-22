# Curio Core scope diff

Pinned against `filecoin-project/curio@integ/task` head `21531097` (2026-05-22 "build ctx"). Will re-confirm SHA with Andy before locking it in.

## Top-level task packages

Curio's `cmd/curio/tasks/tasks.go` wires the following task packages (from `tasks/`):

| Package | In Curio Core? | Reason |
|---|---|---|
| `pdp` | **YES** | The whole point. PDP v1 protocol. |
| `pdpv0` | **YES** | The whole point. PDP v0 protocol (older but still in production). |
| `message` | **YES** | Chain message submission; both PDPs depend on it directly. |
| `tasknames` | **YES** | Small registry used by harmonytask. Trivial dep. |
| `balancemgr` | NO | Cross-task balance management. Optional even in upstream Curio. |
| `expmgr` | NO | Experiment manager. Not load-bearing. |
| `f3` | NO | F3 participation (voting). Lantern handles F3 from the read side; we don't vote. |
| `gc` | NO | Storage garbage collection (sealing artifacts). |
| `indexing` | NO | Deal index for non-PDP retrieval. |
| `metadata` | NO | Sector / piece metadata tracking. |
| `pay` | NO | Payment channel work for the Filecoin retrieval market. |
| `proofshare` | NO | Proof-sharing across nodes. We're single-node. |
| `scrub` | NO | Sealing-artifact verification. |
| `seal` | NO | The sealing pipeline. |
| `sealsupra` | NO | Supranational sealing acceleration. |
| `snap` | NO | Snap deals (snap-up sealing). |
| `storage-market` | NO | Boost / market task surface (non-PDP retrieval). |
| `unseal` | NO | Unseal pipeline. |
| `winning` | NO | WinningPoSt for block production. |
| `window` | NO | WindowPoSt for sealing maintenance. |

## Supporting library deps (from `lib/`)

Imports pulled in transitively from `tasks/pdp` + `tasks/pdpv0`:

| Package | Notes |
|---|---|
| `lib/cachedreader` | LRU-cached piece reader. Required. |
| `lib/chainsched` | Chain event scheduler. Required. |
| `lib/dealdata` | Used by pdpv0 only. Required. |
| `lib/ethchain` | EVM chain helpers (for the PDP contract calls). Required. |
| `lib/filecoinpayment` | Used by pdpv0 only. Required. |
| **`lib/ffi`** | **⚠ Risk.** Wraps `filecoin-project/filecoin-ffi` (CGo + Rust). PDP uses `SealCalls.PieceReader` only — a storage-read code path, NOT the CGo sealing math. Need to carve out the pure-Go subset before integrating. **Plan:** create a `lib/ffi-readonly` vendor in curio-core that exports only `PieceReader` and pulls no CGo. |
| `lib/parkpiece` | Piece-park storage abstraction. Required. |
| `lib/passcall` | Helper for chained task call passthroughs. Required. |
| `lib/paths` | Path conventions. Required. |
| `lib/promise` | Promise/future helper. Required. |
| `lib/proof` | Proof helpers (commp calculation, NOT proving math). Pure-Go from quick scan. |
| `lib/storiface` | Storage interfaces. Pulls some sealing-only types we won't use; should still compile. |
| `lib/urlhelper` | URL parsing. Trivial. |
| `pdp/` + `pdp/contract/` + `pdp/contract/FWSS` | PDP on-chain contract bindings. Required. |
| `market/indexstore` | Used by pdp + pdpv0 for piece location lookup. Required. |
| `market/mk20` | Used by pdp only. Mk20 deal protocol types. Required. |
| `market/ipni/ipniculib` | Used by pdpv0 only. IPNI helpers. Required. |
| `deps/config` | pdpv0 only — typed config struct. Required. |

## Harmony layer

All from `harmony/`:

| Package | Notes |
|---|---|
| `harmonytask` | Required. Use the integ/task layout: Scheduler + DB; skip Peering. |
| `harmonydb` | Required. **Port from Postgres to modernc.org/sqlite.** |
| `harmonydb/sql/*.sql` | ~15-25 of 118 migrations are PDP-relevant (see SQL diff below). |
| `resources` | Required. |
| `taskhelp` | Required. |

## SQL migration scope (preliminary)

The full list of 118 migration files lives at `harmony/harmonydb/sql/`. Walking through them:

**Required for PDP + harmonytask:**
- `20230719-harmony.sql` (core harmony schema)
- `20231103-chain_sends.sql` (chain message tracking)
- `20231225-message-waits.sql` (message wait queue)
- `20240212-common-layers.sql` (config layers)
- `20240228-piece-park.sql` (piece storage)
- `20240416-harmony_singleton_task.sql` (singleton task lock)
- Plus PDP-specific migrations (need to grep `pdp_*` in migration filenames; ~10-15 files)

**Required for harmonytask plumbing:**
- `20240404-machine_detail.sql` (per-node task tracking)
- `20240420-web-task-indexes.sql` (UI task list indexes)

**To investigate:**
- Any migration touching tables that PDP-only schema doesn't depend on but harmonytask does — those still need porting.

**Hard skip:**
- Anything `wdpost`, `sdr-pipeline`, `winning`, `sealing-*`, `snap-*`, `unseal`, `sector-*`, `mining`, `precommit`, `commit`, `provecommit` (sealing pipeline)
- Anything `market-*` for non-PDP retrieval

I'll do the actual file-by-file pass tomorrow when I start the SQL port; this is the rough sketch.

## The CGo carve-out (the one real risk)

`lib/ffi/sdr_funcs.go` defines the `SealCalls` type which wraps `filecoin-project/filecoin-ffi`. PDP only calls `SealCalls.PieceReader`, which:

- Takes a piece CID
- Opens the stored file from the storage layer
- Returns an io.Reader

That's a file-I/O code path. The CGo dependency in `SealCalls` is for `EncodeReplica`, `SealPreCommit*`, `SealCommit*`, `WindowPoSt*` etc. — all of which PDP doesn't touch.

**Plan:** define a minimal interface in curio-core, e.g.:

```go
type PieceReadProvider interface {
    PieceReader(ctx context.Context, pieceCID cid.Cid) (io.Reader, error)
}
```

Provide a pure-Go implementation that reads from the piece-park storage directly. Pass it into the PDP tasks instead of `*ffi.SealCalls`. Then we don't need `lib/ffi` at all.

This is the load-bearing engineering work for the pure-Go claim. Bigger than I'd hoped, but the surface area is small (one method).

## What changes about the Day-1 estimate

I said tomorrow I'd produce this doc + confirm the SHA. Done in 2 hours; the SQL scope diff is the only thing that's still "preliminary." The CGo carve-out is now the gating item for Day 2-3 (bones).

## Updated plan for the rest of the sprint

1. ✅ Day 1: Recon. (This doc.)
2. Day 2: Confirm `integ/task` SHA with Andy via #11 comment. Begin assembly of the bundle. Build the `PieceReadProvider` interface + pure-Go piece-park backing.
3. Day 3: Stub the harmonytask DB driver with noop / in-memory for first compile.
4. Day 4-5: SQLite port via modernc.org/sqlite. The 15-25 migration files.
5. Day 6-7: PDP test port.
6. Day 8: Demo.

CGo carve-out work happens on Day 2 alongside the assembly; if it's harder than I think it pushes Day 3 work to Day 4. Total sprint length still ~one week.
