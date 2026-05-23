# Day 6 — lotus carve-out start + PDP v1 nil-guards + live v1 harvest

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11).
Lotus carve-out tracking: [Reiers/lantern#19](https://github.com/Reiers/lantern/issues/19).
Date: 2026-05-23.

> Reading note: Day 6's *original* acceptance bundle promised "engine
> actually starts, accepts tasks, drops `pdp_full_carveout` tag." Reality:
> the harmonytask DB-seam refactor is **substantially deeper** than the
> Day 5 estimate suggested, and the lotus carve-out for the PDPv0
> transitive closure is **much wider** than the analogous Curio
> `lib/paths` split. Day 6 ships the bounded, durable wins (lotus
> `storage/paths` carve-out, PDP v1 ctor nil-guards, live v1
> `TypeDetails()` harvest, lotus fork plumbed through `replace`) and
> defers the two hard items with honest scoping notes. Better partial +
> honest than fake-finished.

## What shipped

### Phase A — `Reiers/blooms` (the Reiers/lotus fork)

`Reiers/lotus` already exists in GitHub but was renamed to
`Reiers/blooms` long ago (and is currently an ancient 2020 lotus
checkout). `gh repo view Reiers/lotus` follows the redirect; the working
SSH/HTTPS URL is `Reiers/blooms`. We have admin permissions on it. The
master branch is not at lotus v1.36.0 (it's at v0.2.x from 2020), so the
right move is to add a fresh `pdp-pure-go` branch pinned at the
filecoin-project/lotus v1.36.0 SHA, not to use the existing master.

That's what Day 6 did:

- Cloned `filecoin-project/lotus` at tag `v1.36.0` (SHA 154c0c3),
  branched as `pdp-pure-go`, set the remote to `Reiers/blooms`, pushed.
- Applied a `storage/paths/local.go` split using the same pattern as the
  Reiers/curio fork's `lib/paths` split:
  - `storage/paths/local_cgo.go`: `//go:build cgo` — holds
    `GenerateSingleVanillaProof` + `GeneratePoRepVanillaProof`
    (the two filecoin-ffi-bound methods on `*Local`). Byte-identical to
    the upstream method bodies; only the file location moved.
  - `storage/paths/local_nocgo.go`: `//go:build !cgo` — stubs returning
    `errPathsNotBuiltWithCGo`. PDP-only flows never call these.
  - `storage/paths/local.go`: ffi import + the two methods removed; the
    rest of the file is unchanged. `cid` import removed (no longer
    needed in the trimmed file). `var _ Store = &Local{}` interface
    assertion preserved.

**Acceptance**: `CGO_ENABLED=0 go build ./storage/paths/...` from the
lotus-fork root is **green**. `CGO_ENABLED=1` is byte-identical to
upstream when the `extern/filecoin-ffi` submodule is checked out (same
imports, same function bodies, same call sites); without the submodule
both upstream and our fork fail with the same "no such file or
directory" on `extern/filecoin-ffi/go.mod`.

Commit: **`baf8b69`** on `Reiers/blooms` branch `pdp-pure-go`.

### Phase B — `Reiers/curio` fork (extend `integ/task-pdp-pure-go`)

**1. gosigar carve-out — N/A in Reiers/curio.** A full grep of the
Reiers/curio tree for `gosigar`, `elastic/gosigar`, or `"gosigar"` came
back empty. Curio has no direct gosigar imports; gosigar arrives
transitively from `lotus/system`, `lotus/node/repo`, and
`lotus/cli/util` (when those packages are pulled in). The carve-out
therefore needs to land in *lotus*, not curio. See "Deferred work"
below.

**2. `harmonytask.New(...)` DB interface refactor — deferred.**
The Day 5 estimate ("~150 LoC + a re-pin of curiostorage/harmonyquery")
significantly underestimated the surface area. The actual refactor has
to cover:

- `harmonytask.New(db *harmonydb.DB, ...)` *and* `NewWithReg`.
- Every call site inside harmonytask itself (16+ direct uses of `db.`
  / `e.cfg.db.`: `Select`, `Exec`, `QueryRow`, `BeginTransaction`),
  spread across `harmonytask.go`, `scheduler.go`, `task_type_handler.go`,
  `peering.go`.
- The `BeginTransaction` API takes `func(tx *harmonydb.Tx) (bool, error)`
  in its closure signature, meaning `*harmonydb.Tx` is also part of the
  public surface that callers (PDP task code) bake in. Making the DB an
  interface without making the Tx an interface leaves the door half-open.
- `resources.Register(db, hostAndPort)` and
  `resources.CleanupMachines(ctx, db)` in `harmony/resources/` also take
  the concrete `*harmonydb.DB`.
- Helpers like `ht.GetSectorID(db, ...)`.
- `harmonydb.DB` itself is a type alias for `harmonyquery.DB`
  (`github.com/curiostorage/harmonyquery`, a separate module). Either
  we change the alias to wrap an interface, or we restructure the
  alias chain entirely.

A 150-LoC patch isn't sufficient. The right design is to define a
`harmonytask.DB` interface with the ~6-8 methods harmonytask actually
calls, plus a `harmonytask.Tx` interface for the closure type, then
adapt `*harmonydb.DB` and `*harmonysqlite.DB` to both implement them.
But that's a multi-PR workstream touching three modules and the public
API of curio's transaction closure shape. Day 6 didn't attempt this
under the "better partial + honest" rule.

**3. PDP v1 constructor nil-panic fix — done.**
`tasks/pdp/task_init_pp.go`, `task_next_pp.go`, `task_prove.go`: each
constructor now has a `if chainSched == nil { return; }` guard before
the `chainSched.AddHandler(...)` call. The rest of the constructor
runs unchanged. Log at Debug level so a real-world (non-curio-core)
call with a nil chainSched still surfaces in trace output.

Behavior preserved when `chainSched != nil`: byte-identical to upstream.

**Acceptance**: `CGO_ENABLED=0 go build ./tasks/pdp/...` from the
curio-fork root is green.

Commit: **`49ff949`** on `Reiers/curio` branch `integ/task-pdp-pure-go`.

### Phase C — `Reiers/curio-core` (consume new fork APIs)

**1. `go.mod` updates.** Pinned the lotus fork via `replace`:

```
replace github.com/filecoin-project/lotus => github.com/Reiers/blooms v0.2.11-0.20260523003030-baf8b697b916
```

(The `v0.2.11-0` pseudo-version is forced by Go's module resolver:
Reiers/blooms's most recent real tag is `v0.2.10` from 2020, and the
pseudo-version must be `>=` the preceding tag. `v0.2.11-0.<date>-<sha>`
satisfies the requirement; the module-cache resolves the SHA correctly
because the replace directive overrides the import path entirely.)

Also updated the existing curio replace to point at `49ff949`:

```
replace github.com/filecoin-project/curio => github.com/Reiers/curio v1.27.3-0.20260523003309-49ff949b7c1d
```

**2. Live `TypeDetails()` harvest for PDP v1.** With the curio fork's
nil-guards in place, `internal/engine/engine.go`'s `BuildTaskRegistry`
no longer needs the three static `TaskTypeDetails` literals for
PDPInitPP / PDPProvingPeriod / PDPProve. They moved into the `safeCtors`
loop as direct `pdp.NewInitProvingPeriodTask(nil,nil,nil,nil,nil).TypeDetails()`
calls. The registry still ends up with 21 task types (12 v1 + 9 v0)
but now **all 12 v1 descriptors come from the upstream source of
truth**; only the 9 v0 ones remain static. (Drift surface shrunk by
20%, more importantly any future TypeDetails edits to those three
upstream tasks will propagate automatically.)

**3. `pdp_full_carveout` tag still in place.** Reason: dropping it
exposes two transitive blockers that Day 6's carve-out did NOT cover:

- `github.com/elastic/gosigar`'s `sigar_darwin.go` uses `sysctlbyname`
  which is undefined under `CGO_ENABLED=0`. Plus 7 method-name
  undefineds on Cpu/LoadAverage/Mem/Swap/HugeTLBPages/FDUsage,
  apparently due to a version mismatch in our resolution
  (`gosigar@v0.14.3` is failing to compile on master). This is a
  gosigar version-pin problem that needs its own investigation.
- `lotus/storage/sealer` (now `github.com/Reiers/blooms/storage/sealer`)
  has 10+ undefineds in `worker_local.go`, `faults.go`, `manager.go`,
  `manager_post.go` — all `ffi.*` and `ffiwrapper.*` symbols, all CGo.
  The Day 6 carve-out only touched `storage/paths/local.go`; the
  sealer layer needs the same `*_cgo.go` / `*_nocgo.go` split for
  *every* file that imports filecoin-ffi (10+ files), which is a full
  carve-out workstream of its own.

**4. `cmd/curio-core run` boot.** Verified end-to-end against a fresh
`/tmp/cc-test-day6` data-dir:

```
curio-core run: starting daemon
  data-dir: /tmp/cc-test-day6
  db-path:  /tmp/cc-test-day6/state.sqlite
  listen:   127.0.0.1:14712
  engine:   21 task types registered
Setup required. Open http://127.0.0.1:14712/setup in a browser to complete.
  lantern:  skipped (--no-lantern)
  webui:    http://127.0.0.1:14712/

curio-core is running. Ctrl-C to stop.

received terminated; shutting down...
Stopped cleanly.
```

Clean boot, clean shutdown, no panics. The harmonytask scheduler
goroutine is still not started (DB seam refactor not done), so "engine
accepts tasks" hasn't been demonstrated. Day 7 picks this up.

## Acceptance gate status

| Gate | Day 6 status |
|---|---|
| `CGO_ENABLED=0 go build ./...` (curio-core) | **green** |
| `CGO_ENABLED=0 go test ./internal/... ./cmd/...` (curio-core) | **green** |
| `CGO_ENABLED=0 go build ./storage/paths/...` (lotus-fork) | **green** |
| `CGO_ENABLED=0 go build ./tasks/pdp/...` (curio-fork) | **green** |
| `curio-core run` boots + clean shutdown | **green** |
| harmonytask engine goroutine actually starts | **deferred** (DB seam refactor) |
| `pdp_full_carveout` tag dropped | **deferred** (lotus/sealer + gosigar) |

## Deferred work / Day 7+ candidates

1. **harmonytask DB-seam interface refactor.** Scope expanded from
   ~150 LoC to a multi-PR workstream across three modules. Design
   sketch: define `harmonytask.DB` (interface, ~7 methods) and
   `harmonytask.Tx` (interface, ~4 methods) and adapt both
   `*harmonydb.DB` (concrete pgx pool) and our `*harmonysqlite.DB`
   (modernc.org/sqlite) to satisfy them. The `harmonyquery` module
   provides `*harmonydb.DB` and `*harmonydb.Tx` as type aliases —
   either it adopts the interfaces directly or we layer a small
   adapter package on top. Then `harmonytask.New(eng.DB(), impls,
   ..., nil)` becomes a one-line wire-up. Adjacent: `resources.Register`
   has the same DB-seam problem.

2. **`lotus/storage/sealer` CGo carve-out.** 10+ files import
   `filecoin-ffi` directly (worker_local.go, faults.go, manager.go,
   manager_post.go, ffiwrapper/*, supraseal/*, etc.). The carve-out
   needs the same `*_cgo.go` / `*_nocgo.go` split for every one. PDP
   doesn't need any of these at runtime, but `tasks/pdpv0`'s import
   closure pulls them in transitively, so structural stubs under
   `!cgo` are required. Estimated: 1-2 days of focused work, all in
   the Reiers/blooms (lotus) fork.

3. **gosigar version-pin investigation.** `gosigar@v0.14.3` is failing
   to compile under `CGO_ENABLED=0` on darwin with both
   "sysctlbyname undefined" and 7 method-name undefineds on
   Cpu/LoadAverage/Mem/Swap/HugeTLBPages/FDUsage. The undefineds on
   the latter look like a pure-Go interface contract drift; need to
   check whether a different `gosigar` version (or fork) compiles
   cleanly. Possibly easier to replace gosigar transitively with a
   shim in lotus/system (gosigar's lotus call sites are small).

4. **Drop the `pdp_full_carveout` tag.** Once #2 and #3 land, dropping
   the tag should be a one-liner and the existing compile-only +
   error-detection tests in `internal/pdptests/` will run under
   `CGO_ENABLED=0` by default.

## Files touched (curio-core)

- `go.mod`, `go.sum`: lotus replace added, curio replace bumped.
- `internal/engine/engine.go`: three static-literal v1 descriptors
  replaced by live `pdp.NewXxx().TypeDetails()` calls inside
  `safeCtors`. Net: -25 LoC, same behavior, less drift surface.
- `docs/STATUS.md`: updated "What works today" + "What doesn't work yet".
- `docs/DAY-6-NOTES.md`: this file.

## Commit log

```
(this commit)  engine: live PDP v1 TypeDetails harvest + lotus-fork replace (Day 6)
2d0b569        design: Curio Core mark + wordmark + README rewrite
951613f        harmonysqlite: SQL classification + Postgres→SQLite migration port (Day 3)
...
```

(See `docs/STATUS.md` for the full commit-log highlights.)
