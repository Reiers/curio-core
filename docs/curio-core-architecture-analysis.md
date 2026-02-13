# Curio Core architecture analysis: Lotus vs Venus vs Forest

Date: 2026-02-13  
Repos analyzed:
- Lotus (`/Users/reiers/Desktop/lotus`, commit `a554b8c1c`)
- Venus (`/Users/reiers/Desktop/venus`, commit `6933e5c74`)
- Forest (`/Users/reiers/Desktop/forest`, commit `e757bc74d`)

---

## Executive summary

For a **hybrid Curio Core node**, the most pragmatic path is:
1. Treat **Lotus consensus/state semantics** as canonical compatibility reference.
2. Reuse/selectively port **Forest networking/sync orchestration ideas** (tasked state-machine sync, adaptive chain-exchange behavior, stateless mode pattern).
3. Reuse **Venus decomposition concepts** (dispatcher/syncer split and interface-based boundaries), but avoid inheriting its ecosystem fragmentation risk as a base runtime.

Recommended long-term architecture:
- **Go control-plane + RPC compatibility layer** (Lotus-compatible APIs, ops tooling, ecosystem integration).
- **Rust execution/data-plane modules** where there is clear value (high-throughput sync pipeline, chain exchange batching, optional state transition workers, analytics/indexing).
- Keep **consensus-critical determinism boundary** narrow and auditable; avoid speculative FFI in inner message execution path until conformance harness is complete.

---

## Phase 1 — per-repo deep understanding

## 1) Lotus deep architecture

### 1.1 Core architecture, lifecycle, RPC/IPC, storage, networking
- `Syncer` is the consensus-facing orchestrator and explicitly delegates worker scheduling to `SyncManager` (`chain/sync.go:62-80`, `chain/sync.go:85-119`).
- `SyncManager` handles bootstrap peer threshold, queueing/dedup, and concurrent workers (`chain/sync_manager.go:48-67`, `chain/sync_manager.go:178-239`).
- `ChainStore` is the persistence/control hinge: split chain/state blockstores, metadata DS, ARC caches, head-change pubsub, reorg callbacks (`chain/store/store.go:91-134`, `chain/store/store.go:169-201`).
- Networking stack uses libp2p gossipsub with tuned peer scoring and topic score params (`node/modules/lp2p/pubsub.go:31-43`, `node/modules/lp2p/pubsub.go:148-224`).

### 1.2 Chain sync internals
- Fork-choice documented in-code: heaviest chain constrained by finality window (`chain/sync.go:82-84`).
- New head ingestion: bad block gate + msg-meta validation + local persist + weight pre-check + schedule (`chain/sync.go:197-253`).
- Bad blocks and checkpoint/finality protections are integrated in sync flow (`chain/sync.go` around bad/cache/checkpoint logic, e.g., `734`, `871`, `890`, `1288`).

### 1.3 Consensus + VM abstractions
- State transition control is in `StateManager` with network-version schedule and migration registry (`chain/stmgr/stmgr.go:131-167`, `chain/stmgr/stmgr.go:187-217`).
- Executor abstraction (`chain/stmgr/stmgr.go:120-123`) is a major modularity seam.
- Legacy VM charges gas at IPLD operations (`chain/vm/vm.go:83-127`) and enforces runtime call depth (`chain/vm/vm.go:171-173`).
- Legacy VM explicitly not for NV16+ (`chain/vm/vm.go:258-260`), so modern path depends on FVM integration.

### 1.4 State tree / actors / AMT-HAMT
- State-tree version evolution and loading per actor version family is explicit (`chain/state/statetree.go:144-166`, `chain/state/statetree.go:183-225`, `chain/state/statetree.go:253-293`).
- Snapshot layering in state tree shows mutation staging strategy (`chain/state/statetree.go:44-99`).

### 1.5 Performance, memory, concurrency model
- ARC caches for tipsets/msg-meta (`chain/store/store.go:97-100`, `124-126`).
- Sync work split across manager workers; state execution caching (`chain/stmgr/stmgr.go:161-167`).
- Main bottlenecks: state transition CPU+IO, chain exchange latency, block/msg validation fan-out.

### 1.6 Modularity / swappable boundaries
Best seams:
- `Syncer` ↔ `SyncManager` boundary (`chain/sync.go:121`, `chain/sync_manager.go:54-67`).
- `ChainStore.WeightFunc` injection (`chain/store/store.go:89`, `105`).
- `StateManager.Executor` (`chain/stmgr/stmgr.go:120-123`).

---

## 2) Venus deep architecture

### 2.1 Core architecture, lifecycle, RPC/IPC, storage, networking
- Sync subsystem composition is explicit: `Manager -> Dispatcher -> Syncer` (`pkg/chainsync/chainsync.go:34-67`).
- `Dispatcher` owns target queues, concurrency, worker launch/cancellation (`pkg/chainsync/dispatcher/dispatcher.go:65-99`, `193-216`, `266-300`).
- `Store` persists head/checkpoint/tipset metadata and emits head events (`pkg/chain/store.go:89-136`).
- Libp2p/gossipsub stack is Lotus-like with peer scoring, peer gater, allowlist filters (`pkg/net/gossipsub.go:63-139`, `211-320`).

### 2.2 Chain sync internals
- `Syncer` comments acknowledge EC coupling and assumptions (`pkg/chainsync/syncer/syncer.go:47-53`).
- `syncOne` validates each block (errgroup), updates trackers, persists tipset key, records metrics (`pkg/chainsync/syncer/syncer.go:191-258`).
- Transition mismatch rollback path exists but is marked “deprecated / needs considerations” in state manager (`pkg/statemanger/state_manger.go:229-243`).

### 2.3 Consensus + VM abstractions
- Interfaces for `StateProcessor`, `BlockValidator`, `ChainReaderWriter`, `ChainSelector` improve decoupling (`pkg/chainsync/syncer/syncer.go:79-120`).
- `Stmgr` centralizes transition execution, caching, and fork/gas/syscall context (`pkg/statemanger/state_manger.go:59-86`, `245-296`).
- Legacy VM wrapper also blocks NV16+ (`pkg/vm/vm.go:38-40`).

### 2.4 State tree / actors / AMT-HAMT
- State-tree implementation mirrors Lotus patterns: version mapping + per-version trees (`pkg/state/tree/state.go:132-153`, `170-212`, `239-278`).

### 2.5 Performance, memory, concurrency model
- Target-tracker + dispatcher queue design is simple and operationally understandable.
- `RunStateTransition` has explicit in-flight per-tipset synchronization channels (`pkg/statemanger/state_manger.go:252-281`).
- Potential pressure points: lock contention in `stLk`, rollback semantics, and complex dependency surface due to multi-component ecosystem.

### 2.6 Modularity / swappable boundaries
Best seams:
- `dispatchSyncer` interface (`pkg/chainsync/dispatcher/dispatcher.go:31-37`).
- `Stmgr` transition API (`pkg/statemanger/state_manger.go:245-277`).
- Chain store abstraction points (`pkg/chain/store.go:87`, `135`).

---

## 3) Forest deep architecture

### 3.1 Core architecture, lifecycle, RPC/IPC, storage, networking
- Sync is implemented as a **state-machine + async tasks** (ingest, update state, spawn fetch/validate tasks) (`src/chain_sync/chain_follower.rs:128-149`, `221-273`).
- Supports stateless sync mode by design (`src/chain_sync/chain_follower.rs:72-76`).
- `ChainStore` is thread-safe with broadcast head changes + validated-block set (`src/chain/store/chain_store.rs:55-87`).
- Libp2p behavior composes gossipsub, bitswap, hello, chain exchange in one behavior object (`src/libp2p/behaviour.rs:116-177`).

### 3.2 Chain sync internals
- `validate_tipset` validates blocks in parallel and bad-block policy is explicit (`src/chain_sync/tipset_syncer.rs:93-147`).
- `validate_block` performs:
  - parent tipset load and lookback resolution (`196-215`)
  - base fee + weight checks (`232-269`)
  - computed state+receipt root checks (`271-299`)
  - consensus validation call (`310-337`)
- `SyncNetworkContext` has adaptive timeout and bounded concurrent “race peers” chain exchange (`src/chain_sync/network_context.rs:37-46`, `70-116`, `238-320`).

### 3.3 Consensus + VM abstractions
- `StateManager` wraps chain store, tipset-state cache, beacon, multi-engine (`src/state_manager/mod.rs:162-171`).
- Rewind safety for actor bundle mismatch (strong operational guard) (`src/state_manager/mod.rs:212-259`).
- Interpreter shims across FVM2/FVM3/FVM4 (`src/interpreter/mod.rs:4-8`, `src/shim/state_tree.rs:137-146`).

### 3.4 State tree / actors / AMT-HAMT
- State tree dynamically resolves supported versions and backends (`src/shim/state_tree.rs:152-185`).
- Explicit handling differences for delegated address support across FVM versions (`src/shim/state_tree.rs:207-253`).

### 3.5 Performance, memory, concurrency model
- Rust async + join sets + bounded channels and explicit synchronization points.
- Request racing improves tail-latency resilience in chain exchange.
- Strong modularity, but operational complexity can move into async task orchestration/observability.

### 3.6 Modularity / swappable boundaries
Best seams:
- `SyncNetworkContext` for network fetch strategy (`src/chain_sync/network_context.rs:47-58`).
- Task-based sync state machine in `chain_follower`.
- `StateManager` + `StateLookupPolicy` usage in sync validator (`src/chain_sync/tipset_syncer.rs:277-299`).

---

## Phase 2 — comparative matrix

| Subsystem | Lotus | Venus | Forest | Best candidate (for Curio Core) |
|---|---|---|---|---|
| Consensus correctness baseline | Canonical in ecosystem | Compatible derivative | Strong, but Rust parity burden | **Lotus (reference baseline)** |
| Sync scheduler model | Syncer + SyncManager worker model | Dispatcher + target tracker | State-machine task graph | **Forest design**, Lotus semantics |
| Chain store & head/reorg eventing | Mature, battle-tested | Similar design | Clean Rust abstraction + broadcast | **Lotus for compatibility**, Forest patterns for redesign |
| Bad block handling | Integrated cache + checkpoint/finality guards | Bad tipset cache + rollback path | Explicit cache + policy branching | **Lotus/Forest hybrid** |
| State manager architecture | Rich migrations/version schedule | Similar + interface-heavy | Cache + rewind guards + multi-engine | **Lotus semantics + Forest guard patterns** |
| VM integration strategy | Legacy + FVM split | Legacy wrapper + FVM path | Multi-FVM shim enum | **Forest for abstraction, Lotus for behavior parity** |
| Libp2p/pubsub scoring | Mature and tuned | Similar to Lotus | Similar, with noted tradeoffs | **Lotus/Venus params as baseline** |
| Chain exchange strategy | Mature peer model | Conventional exchange client | Adaptive timeout + peer racing | **Forest** |
| Stateless mode | Not first-class | Not first-class | First-class | **Forest** |
| RPC ecosystem compatibility | Highest | moderate/high (ecosystem split) | improving; Lotus-json shims | **Lotus-compatible facade** |
| Modularity for extraction | Medium (fx graph complexity) | Medium-high | High | **Forest/Venus patterns** |
| Language/runtime profile | Go | Go | Rust | **Hybrid** |

---

## Phase 3 — extraction plan

### 3.1 Subsystem extraction feasibility

**High feasibility**
- Network chain-exchange client/scheduler (Forest-inspired) behind stable protobuf/json boundary.
- Sync target orchestration layer (Forest task-state-machine or Venus dispatcher style).
- Observability/metrics pipelines and sync status reporting.

**Medium feasibility**
- ChainStore replacement while keeping Lotus-compatible API surface.
- Bad-block cache/checkpoint/finality policy module.

**Low feasibility (initially)**
- Consensus-critical message execution path across language boundary.
- Full VM/state transition outsourcing to Rust before extensive conformance parity.

### 3.2 Required refactors
- Define explicit Curio Core internal interfaces:
  - `ChainDataStore`, `HeadTracker`, `ForkChoice`, `TipsetValidator`, `StateTransitionEngine`, `NetworkFetcher`.
- Remove direct concrete coupling in orchestration code (Lotus fx-wired components and globals).
- Introduce deterministic “state transition witness” format for cross-impl verification.

### 3.3 Licensing implications
- Lotus: dual Apache-2.0 / MIT.
- Venus: dual Apache-2.0 / MIT.
- Forest: MIT OR Apache-2.0 (`Cargo.toml`: `license = "MIT OR Apache-2.0"`).

=> License-compatible for reuse in Curio Core under Apache-2.0/MIT dual strategy.  
Watch transitive dependencies with different terms (especially optional crates/libs and FFI-linked native libs).

### 3.4 Technical incompatibilities (Go/Rust)
- Memory ownership and zero-copy assumptions differ (Go GC vs Rust ownership/borrowing).
- Cross-language FFI for high-frequency hot path (message execution) risks latency + complexity.
- Error model mismatch (`error` wrapping vs typed Rust enums) impacts diagnostics bridging.
- Serialization canonicalization must be byte-identical (CID roots/state roots are unforgiving).

### 3.5 Reimplement vs reuse recommendations
- **Reuse semantics, not code, for consensus-critical logic** unless embedding one runtime as authoritative.
- **Reuse/port architecture patterns**:
  - Forest: tasked sync, adaptive chain exchange, stateless mode.
  - Venus: dispatcher contracts.
- **Reimplement glue layers** for Curio-specific boundaries and observability.

---

## Phase 4 — Curio Core blueprint

### 4.1 Long-term production architecture

- **Core-API Plane (Go)**
  - Lotus-compatible JSON-RPC + auth + admin APIs.
  - Node lifecycle/orchestration.
  - Config/upgrade controller.

- **Consensus Plane (Go first, Rust optional later)**
  - Fork-choice, checkpoint/finality guards, bad block policy.
  - Deterministic validation coordinator.

- **Execution Plane (pluggable)**
  - Engine A: canonical execution (initially Lotus/FVM-compatible behavior in Go path).
  - Engine B: experimental Rust execution worker behind deterministic equivalence checks.

- **Data Plane (Rust-leaning)**
  - Chain exchange fetcher + request racing.
  - Sync task graph and backpressure-aware queues.
  - Optional high-throughput indexers.

### 4.2 Module breakdown
- `curio-rpc` (Lotus API compatibility)
- `curio-sync-orchestrator`
- `curio-chainstore`
- `curio-consensus-policy`
- `curio-state-transition`
- `curio-network-fetch`
- `curio-observability`
- `curio-upgrade-manager`

### 4.3 Language strategy + FFI boundaries
- Phase A: all consensus-critical path in one language/runtime boundary.
- Phase B: add Rust modules for fetch/sync scheduling/indexing first.
- Phase C: optional execution coprocessor mode (shadow execution + hash compare before cutover).
- FFI boundary must pass compact immutable payloads (tipset key, block headers, msg roots, receipts roots), not giant object graphs.

### 4.4 State management
- Keep state root as single source of truth.
- Tipset-level state cache with bounded size and warmup strategy (Forest-like cache prepopulation concept).
- Persist deterministic transition artifacts for replay/audit.

### 4.5 Observability
- Mandatory per-tipset spans: fetch, validate, execute, commit.
- Distinguish network vs lookup vs execution failures (Forest network_context failure taxonomy is a good model).
- Export Prometheus + structured logs + trace IDs across Go/Rust boundary.

### 4.6 Upgrade strategy
- Explicit network-version schedule and migration registry (Lotus/Venus model).
- Pre-migration dry-run and cache hooks.
- “Rewind-safe” checks for actor bundle/version mismatch (Forest pattern).

### 4.7 Testing strategy
- Golden vector conformance against Lotus canonical outputs.
- Differential testing: Curio vs Lotus on same CAR segments.
- Shadow mode in production: execute both paths, compare roots/receipts/events.
- Fault-injection in chain exchange and reorg storms.

### 4.8 Backward/RPC compatibility plan
- Start with strict Lotus JSON-RPC method compatibility for critical APIs (`Chain*`, `State*`, `Mpool*`, `Net*`).
- Maintain response shape compatibility (including edge-case error strings where clients depend on them).
- Version-gate optional Curio extensions under separate namespaces.

---

## Phase 5 — risk assessment

### 5.1 Technical risks
- Consensus divergence from subtle serialization/state differences.
- Reorg corner-case handling differences under heavy churn.
- FFI boundary introducing nondeterministic behavior or hidden retries.

### 5.2 Ecosystem risks
- Client tooling assumes Lotus quirks; strict spec compatibility may still fail practical compatibility.
- Rapid actor/network upgrades can invalidate assumptions across mixed-language modules.

### 5.3 Maintenance risks
- Dual-language team cost and debugging burden.
- Upstream drift from Lotus/FVM changes requiring constant parity work.

### 5.4 Fork divergence risks
- Venus and Forest evolve independently; cherry-picking design ideas is safer than deep code coupling.
- Copying too much non-canonical logic increases long-term merge/conflict debt.

### 5.5 Consensus compatibility risks
- Any independent reimplementation of execution/fork logic is high risk without continuous differential validation.
- Recommended mitigation: keep one canonical execution path authoritative until long soak + proof of equivalence.

---

## Curio Core recommended implementation sequence

1. Build Lotus-compatible RPC + storage facade.  
2. Integrate Forest-style sync orchestration and adaptive chain exchange in non-consensus-critical path.  
3. Keep canonical state transition path conservative; add shadow execution comparator.  
4. Introduce optional Rust execution only after sustained zero-diff across long historical + live windows.  
5. Formalize upgrade runbooks and rollback/rewind mechanics before production cutover.

---

## Explicit deeper inspection still needed

1. **Lotus FX graph + runtime wiring** (precise dependency graph and startup sequencing):
   - `node/`, `cmd/lotus/`, `node/modules/*` (only pubsub module deeply sampled here).
2. **Lotus modern FVM integration path** beyond legacy VM file sampled (`chain/vm/vm.go` indicates cutoff only).
3. **Venus component split and production topology** (`venus-component/`, `venus-shared/`, `app/submodule/*`) to map deploy-time boundaries.
4. **Forest RPC compatibility coverage vs Lotus** across full method matrix (`src/rpc/methods/*`).
5. **Consensus fault/slash filtering interactions** in each implementation under adversarial scenarios:
   - Venus: `pkg/chainsync/slashfilter/*`
   - Lotus/Forest equivalent paths need side-by-side test harness mapping.
6. **Benchmark-grade profiling** (CPU, memory, lock contention, p2p bandwidth) under identical replay workloads; this document is architectural/code-level, not benchmark-validated.

---

## Bottom line

- **Use Lotus as compatibility truth.**
- **Adopt Forest’s modern sync/network architecture patterns.**
- **Use Venus primarily as decomposition inspiration, not as the primary base.**
- **Ship Curio Core as a staged hybrid with strict differential validation before moving consensus-critical execution across language boundaries.**
