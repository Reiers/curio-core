# Day 5 — engine wire-up + first-run config

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11).
Date: 2026-05-23.

## What shipped

### `internal/engine` — engine surface + task registry

- `engine.New(ctx, Config)` opens the SQLite state DB at the configured
  path (`$XDG_DATA_HOME/curio-core/state.sqlite` or `~/.local/share/curio-core/state.sqlite`
  if XDG is unset, with `--db-path` / `Config.DBPath` overrides), applies
  all 14 migrations, and builds a `TaskRegistry` of every PDP v1 + PDP v0
  task type curio-core schedules.
- `engine.Start(ctx)` records the lone `harmony_machines` row (single-server,
  no Peering layer), flips the running flag.
- `engine.Stop()` closes the DB; idempotent.
- `engine.Healthy()` reports the post-Start / pre-Stop window.
- `TaskRegistry` is built once at construction and exposes `Has`, `Get`,
  `Names`, `Len`. Built from a mix of:
  - Live `pdp.NewXxx(nil...).TypeDetails()` calls for PDP v1 task types
    whose constructors don't dereference deps (9 tasks).
  - Static `harmonytask.TaskTypeDetails` literals for PDP v1 tasks whose
    constructors call `chainSched.AddHandler` in-line (3 tasks: PDPInitPP,
    PDPProvingPeriod, PDPProve).
  - Static `harmonytask.TaskTypeDetails` literals for all 9 PDP v0 task
    types (since `tasks/pdpv0` doesn't compile under `CGO_ENABLED=0` yet;
    see "Fork follow-ups" below).
- Total registered: 21 task types (12 v1 + 9 v0).

### `internal/config` — first-run config

- `ConfigBundle` is `[Pdp]{MarketAddress, WalletAddress, MinerID}` TOML.
- `Status(ctx, db)` returns `FirstRunStatus{NeedsSetup, Missing}` reading
  from `harmony_config WHERE title='default'`. Missing fields are listed
  in canonical order: `market_address`, `wallet_address`, `miner_id`.
- `UpsertDefaultLayer(ctx, db, cfg)` validates non-empty + writes via
  SQLite UPSERT. Idempotent; rejects whitespace-only fields before any
  DB hit.

### `internal/setupweb` — first-run WebUI

- A single `Handler` that serves `GET /setup` (inline HTML form) and
  `POST /api/setup` (validates + UpsertDefaultLayer + redirects to `/`).
- Built-in middleware: any non-`/setup` path on an unconfigured DB gets
  a 303 to `/setup`. Once configured, falls through to `Inner` (or a
  small placeholder if `Inner` is nil).
- `/setup` stays reachable even when configured so an operator can
  revisit and update fields.
- Inline HTML template + CSS so the package has zero `embed` surface and
  zero gorilla/mux dependency. Day 6 (or 7) will fold this into the
  full WebUI once the `//go:build cgo` shim comes off `web/`.
- `web/static/pages/setup/index.html` is a static placeholder for that
  future day; the live form is always rendered by `internal/setupweb`.

### `cmd/curio-core run` — daemon CLI

- New subcommand: `curio-core run`.
- Flags: `--data-dir`, `--gateway`, `--db-path`, `--listen`, `--no-lantern`,
  `--lantern-anchor-timeout`. `--help` prints sensible docs.
- Boot sequence:
  1. Open the harmonysqlite state DB (auto-migrates).
  2. `engine.New` + `engine.Start`.
  3. `config.Status` probe. If `NeedsSetup`, prints exactly one line:
     `Setup required. Open http://<listen>/setup in a browser to complete.`
  4. Optionally start the embedded Lantern daemon (skippable via
     `--no-lantern`; useful for offline first-run setup).
  5. Serve the WebUI on the configured listen address.
- Handles SIGINT/SIGTERM: stops the HTTP server → stops Lantern → stops
  the engine (which closes the DB). Verified end-to-end against a fresh
  `/tmp/cc-test` data-dir.

### Tests added

- `internal/engine/engine_test.go` — engine lifecycle, registry contents,
  `harmony_machines` row, XDG resolution.
- `internal/config/firstrun_test.go` — fresh-DB status, partial-bundle
  status, upsert round-trip, validation rejection, encode/decode.
- `internal/setupweb/setupweb_test.go` — redirect, form render, happy-path
  POST + fall-through, validation re-render, method-not-allowed.

Total new tests: 16 top-level cases (3 with sub-tests). All green under
`CGO_ENABLED=0 go test ./internal/... ./cmd/...`.

## Non-trivial design choices

### 1. Why no `harmonytask.New` call yet

Upstream `harmonytask.New(db *harmonydb.DB, ...)` takes a concrete
`*harmonydb.DB`, which is `type DB = harmonyquery.DB` — a pgx-backed
Postgres pool. There is no interface boundary at the DB seam, so plugging
in a SQLite-backed handle is **architecturally impossible without
fork-side changes** to the upstream Curio + curiostorage/harmonyquery
modules.

The Day 5 task explicitly said:

> If you find a missing dep or a surface change needed in the fork,
> document it as a TODO in DAY-5-NOTES.md and proceed with a
> curio-core-side adapter where possible.

So I built the adapter shell — the engine wraps the DB, the registry,
the lifecycle, the machine-row insert — and left `harmonytask.New`
unbuilt. The Start() body has the single growth seam where the real
scheduler will eventually wire in once the upstream DB seam exists. See
"Fork follow-ups" below.

### 2. Why some PDP v1 TaskTypeDetails are static literals

`InitProvingPeriodTask`, `NextProvingPeriodTask` and `ProveTask`
constructors call `chainSched.AddHandler(...)` in their bodies. Passing
`nil` for `chainSched` panics inside the constructor — before
`.TypeDetails()` is ever reachable. Rather than build mock chainSched
adapters that go nowhere, I copied the upstream `TaskTypeDetails`
literal into a static table. The cost is a tiny drift risk if upstream
edits a Max / Cost shape; the benefit is the registry stays a one-call
operation. When the real scheduler wires in (with real chainSched),
these three move back to live `TypeDetails()` harvest in the same loop
as the others.

### 3. Why PDP v0 is entirely static-descriptor

`tasks/pdpv0` transitively imports `curio/lib/paths` → `lotus/storage/paths`
→ both `gosigar` (Darwin sysctlbyname is undefined under
`CGO_ENABLED=0`) and `ffi.GenerateSingleVanillaProof` (CGo-bound). That
is Day 6's carve-out per `docs/PLAN.md`. Until then, the only way to
register pdpv0 task names is from `tasks/tasknames` (which is the leaf
package with zero non-stdlib imports and compiles fine).

The 9 static v0 descriptors are conservative defaults; Day 6 swaps them
for live `pdpv0.NewXxx(...).TypeDetails()` harvests in the same place
as v1.

### 4. `harmony_machines` single-row + no Peering

Curio's upstream `harmony_machines` is a multi-row table (one per node)
joined against by the Peering goroutine to discover peers. curio-core
is single-server: there is one node, and "peering" is a no-op.

The engine keeps `harmony_machines` as a regular row-per-node table to
keep the SQL surface identical to upstream (in case Day 7's integration
test wants to query `JOIN harmony_machines`), but only ever writes a
single row keyed by HostAndPort. The upsert pattern is `UPDATE ... WHERE
host_and_port=?`; if `RowsAffected()` is 0, fall back to INSERT. This
beats SQLite's `INSERT...ON CONFLICT` because `host_and_port` isn't a
unique key in the upstream schema, and we don't want to start adding
indexes that aren't there.

The `peering.go` / `peerConnector` arg to `harmonytask.New` will be
passed `nil` when the scheduler eventually wires in. The upstream
`startPeering(e, nil)` handles a nil connector as a no-op, so the
single-server skip costs zero new code.

### 5. Why `internal/setupweb` is separate from `web/`

`web/srv.go` is under `//go:build cgo` because the WebUI's deps still
pull in `lotus/storage/paths`, `gosigar`, and `curio/deps`. Adding the
first-run middleware to `web/srv.go` would have meant `curio-core run`
couldn't build under `CGO_ENABLED=0`.

`internal/setupweb` is the minimal CGO-free analogue: just the /setup
form + /api/setup POST handler + the middleware that wraps any inner
http.Handler. When Day 6 lifts the CGo shim, the middleware moves into
`web/srv.go` as a top-of-router mw with `setupweb.Handler` as the
explicit /setup mount. Zero new types required for that move.

### 6. No new schema migrations

Day 5 needed no schema changes. The default-layer write reuses the
existing `harmony_config` table from `0006_common_layers.sql`. The
`harmony_machines` row uses the columns from `0001_harmony_core.sql`.

## Fork follow-ups (TODO before Day 6 or 7)

These items are blocked on changes to the `Reiers/curio` fork (or
upstream `curiostorage/harmonyquery`); none were touched here per the
Day 5 constraint:

1. **`harmonydb.DB` → interface seam.** `harmonytask.New` should take an
   interface (covering `Exec`, `Select`, `QueryRow`, `BeginTransaction`,
   `Tx.Exec`/`Tx.Select`/`Tx.QueryRow`) rather than `*harmonydb.DB`. Our
   SQLite handle (`internal/harmonysqlite.DB`) already implements the
   shape upstream uses; once the type loosens, curio-core's
   `engine.Start` calls `harmonytask.New(eng.DB(), impls, ..., nil)` and
   the scheduler is live. **Estimated fork-side scope: ~150 LoC + a
   re-pin of `curiostorage/harmonyquery`.**

2. **`resources.Register` / `resources.Reg`.** Same DB seam problem.
   Once #1 lands, `resources.Register(eng.DB(), eng.HostAndPort)`
   replaces the hand-rolled `recordMachineRow` in `engine.go`. Note:
   curio-core only needs CPU=1, RAM=`1<<30`, GPU=0 so the upstream
   resource-probe shellouts (`elastic/go-sysinfo`, etc.) aren't worth
   pulling in — keep `RegisterWithResources` for that path.

3. **PDPv0 transitive carveout (Day 6).** `tasks/pdpv0` needs
   `lotus/storage/paths` + `gosigar` removed from its import closure.
   Same pattern as the Day 4 `lib/ffi` → `PieceReader` interface swap.
   Once done, the 9 static PDP v0 descriptors in
   `internal/engine/engine.go` become live `pdpv0.NewXxx(nil...).TypeDetails()`
   harvests.

4. **PDP v1 constructor side-effects.** `task_init_pp.go`,
   `task_next_pp.go`, and `task_prove.go` call `chainSched.AddHandler`
   inside their constructors. Consider lifting that registration into a
   separate `(t *Task) Register(chainSched)` method so the constructor
   is side-effect-free. Then the 3 static-literal v1 descriptors join
   the live harvest in `BuildTaskRegistry`. **Optional / cosmetic.**

5. **harmony_config "default" vs "base" layer.** Upstream Curio's
   convention is `title='base'`; curio-core uses `title='default'` per
   the Day 5 spec. If/when curio-core fronts a real Curio config
   load_layer call, the layer name has to align with whatever
   `deps/deps.go`/`deps/config/load.go` expect. May require a fork
   patch to make the layer name configurable. **Verify before Day 7's
   integration test.**

## Acceptance results

- `CGO_ENABLED=0 go build ./...` — **green**
- `CGO_ENABLED=0 go vet ./...` — **green**
- `CGO_ENABLED=0 go test ./internal/... ./cmd/...` — **all green**
- `curio-core run --help` — prints flag docs (verified)
- `curio-core run --data-dir /tmp/cc-test --no-lantern --listen 127.0.0.1:14711`
  end-to-end:
  - "Setup required" line printed on boot ✓
  - `GET /setup` → 200 + form HTML ✓
  - `GET /` → 303 → `/setup` ✓
  - `POST /api/setup` (valid) → 303 → `/` ✓
  - `GET /` post-setup → 200 + fall-through ✓
  - SIGTERM → clean shutdown ✓
  - `state.sqlite` persisted on disk ✓
- Skipped Lantern boot via `--no-lantern` so the test doesn't depend
  on the live gateway; the lantern boot path is unchanged from
  `cmdProbe` and is exercised separately by `curio-core probe`.
