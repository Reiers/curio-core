# Curio Core Capability Gap Matrix (vs Lotus / Venus / Forest)

Date: 2026-02-13

Severity rubric:
- **P0** = cannot sync reliably without it
- **P1** = required for real-world SP traffic / stability
- **P2** = can ship later

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|

## A) Chain Sync

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| Tipset fetch/validation pipeline | Full staged syncer + manager + validator | Full dispatcher/syncer pipeline | Full task-state-machine sync | Snapshot import + `node.StartSkeleton`; no real fetch/validate loop | **P0** | Implement staged sync pipeline: fetch -> header validate -> persist -> candidate head update |
| Bad block handling | Built-in bad block cache/policy | Bad tipset tracking | Explicit bad-block policy/cache | None | **P0** | Add bad block cache + rejection gate before persistence |
| Fork choice | Heaviest-chain with finality guards | Chain selector interfaces | Weight-based with validation gates | None | **P0** | Add weight comparator + head selection interface |
| Reorg handling | Mature reorg notifications/rollback | Head update with rollback logic | Head update state machine | None | **P0** | Add canonical head update + rollback-safe state transitions |
| Chain store persistence | Persistent chain + metadata stores | Persistent store/checkpoint/head | Persistent chain store | Only `imported.snapshot.meta` text file | **P0** | Add chain store + blockstore persistence layout |
| Header vs full validation | Both (staged fast path then full) | Both | Both | No chain validation | **P1** | Add explicit validation stage flags, with header-only bootstrap mode |

## B) State / VM Execution

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| FVM integration / actor execution | Yes | Yes | Yes (multi-FVM shims) | None | **P0** | Add feature-flagged apply stub with deterministic interfaces for future FVM integration |
| Message application | Full | Full | Full | Decode helper only | **P0** | Add message decode registry + execution pipeline stubs + coverage reporting |
| Gas accounting | Full | Full | Full | None | **P1** | Add gas accounting interfaces and test fixtures around message application flow |
| AMT/HAMT data structures | Full | Full | Full | None | **P1** | Add adapters for AMT/HAMT traversal in state module (Go now; Rust sidecar later) |
| State tree/CBOR decoding | Full | Full | Full | None | **P0** | Add state root resolver + basic CBOR decode path |

## C) Networking

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| libp2p host | Yes | Yes | Yes | `network.Supported` only | **P0** | Add minimal libp2p host and peer manager |
| gossipsub | Yes | Yes | Yes | None | **P1** | Add gossipsub subscription for blocks/messages (initial read-only) |
| peer scoring | Yes | Yes | Yes | None | **P1** | Add baseline peer scoring config compatible with Lotus defaults |
| hello/identify/exchange | Yes | Yes | Yes | None | **P0** | Add identify + hello + chain exchange fetch entrypoints |
| bitswap/blockstore strategy | Yes | Partial | Yes | None | **P1** | Add block fetch strategy integrated with blockstore and backpressure |

## D) Storage

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| blockstore | Yes | Yes | Yes | No real blockstore | **P0** | Implement persistent blockstore interface + default local backend |
| chain datastore | Yes | Yes | Yes | No chain datastore | **P0** | Implement chain metadata store (head, tipset index, bad blocks) |
| snapshot import compatibility (Forest assumptions) | Yes | N/A | Native | Imports bytes only; no chain graph/state wiring | **P0** | Parse imported snapshot into blockstore + set canonical head and state roots |
| pruning strategy | Mature options | Some options | Modes include stateless patterns | None | **P2** | Defer; design after stable sync correctness |

## E) RPC / API

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| compatible endpoints (minimal tooling set) | Broad | Broad | Growing | CLI only; no RPC server | **P0** | Add minimal RPC server: status, chain head, message lookup |
| auth model | Token/JWT style controls | Similar | Present | None | **P1** | Add token auth middleware and local admin-only mode |
| admin commands | Yes | Yes | Yes | CLI basics only | **P1** | Add RPC admin endpoints for sync controls and diagnostics |
| metrics endpoint | Yes | Yes | Yes | None | **P1** | Add `/metrics` Prometheus endpoint |

## F) Observability

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| structured logs | Yes | Yes | Yes | Basic logger | **P1** | Add sync-context fields (peer, tipset, CID, stage) |
| metrics (prometheus) | Yes | Yes | Yes | None | **P1** | Add sync lag, fetch failures, head height, queue depth metrics |
| tracing hooks | Partial/varies | Partial/varies | Strong async tracing | None | **P2** | Add OpenTelemetry hooks after core path stabilizes |

## G) Wallet / Signing

| Capability | Lotus | Venus | Forest | Curio Core Today | Gap Severity | What we do |
|---|---|---|---|---|---|---|
| f1/f3/f4 complete | Yes | Yes | Yes | Placeholder address/key flows | **P1** | Replace placeholders with real key derivation + validation |
| f2 resolve | Yes | Yes | Yes | TODO stub | **P1** | Implement on-chain actor resolve via state lookup |
| keystore security model | Mature | Mature | Mature | Local encrypted JSON (alpha) | **P1** | Harden keystore format, locking, and key material policy |
| message signing + send flow | Full | Full | Full | Alpha sign/verify placeholders | **P1** | Add real signing + mpool send path once RPC/network path exists |
