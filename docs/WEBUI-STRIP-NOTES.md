# WebUI Strip Notes (Day 3 partial)

Vendored the Curio WebUI from `Reiers/curio-fork` (integ/task @ 21531097) into
`curio-core/web/` and stripped the panels that Andy flagged as out of scope for
the PDP-only bundle.

Tracking issue: [Reiers/lantern#11](https://github.com/Reiers/lantern/issues/11).

> Andy's directive: "Simplify WebUI to remove any panels for porep or
> clustering but leaving PDP pipeline, system health, network connectivity
> health."

## Build gating

Every `.go` file under `web/` has `//go:build cgo` at the top.

Reason: the WebUI's transitive dep graph still pulls in
`github.com/elastic/gosigar`, `github.com/filecoin-project/lotus/storage/paths`,
and `github.com/filecoin-project/curio/lib/ffi`, all of which only compile under
cgo. The recent `tasks/pdp compiles under CGO_ENABLED=0` milestone did not
detoxify those upstream packages, and doing so is out of scope for the WebUI
strip task.

With the build tag, `CGO_ENABLED=0 go build ./...` skips the `web/` package
entirely and stays green (matching the curio-core CI invariant). When the CGo
toolchain + filecoin-ffi pkg-config is set up locally, a plain `go build ./web/...`
will compile the stripped UI too.

To remove the tag in the future: once `web/` no longer transitively imports
gosigar / lotus storage paths / curio ffi (likely via a Curio-side WebUI
detox), drop the `//go:build cgo` line from each file.

## Static assets deleted (per task spec)

```
web/static/pipeline-porep.mjs
web/static/cc-scheduler.mjs
web/static/cluster-machines.mjs
web/static/cluster-tasks.mjs
web/static/cluster-task-history.mjs
web/static/harmony-task-counts.mjs
web/static/pipeline-stats.mjs
web/static/snap/                      (entire dir)
web/static/sector/                    (entire dir)
web/static/pages/pipeline_porep/      (entire dir)
web/static/pages/sector/              (entire dir)
web/static/pages/snap/                (entire dir)
web/static/pages/deadline/            (entire dir)
web/static/pages/partition/           (entire dir)
web/static/pages/wallet/              (entire dir)
web/static/pages/market-settings/     (entire dir)
```

## Static assets kept

Top-level dashboard widgets (`web/static/*.mjs`):

- `actor-summary.mjs`
- `chain-connectivity.mjs`
- `network-summary.mjs`
- `storage-gc.mjs`
- `storage-use.mjs`
- `win-stats.mjs` *(see "Ambiguous / punted" below)*

Pages (`web/static/pages/`):

- `actor`, `alerts`, `config`, `ipfs-content`, `ipni`, `mk20`, `mk20-deal`,
  `node_info`, `pdp`, `piece`, `storage_path`, `storage_paths`, `task`,
  `upload-status` — all on Andy's keep list.
- `market`, `mk12-deal`, `mk12-deals` — explicitly "still under review with
  Andy, leave in place".
- `proofshare` — not on the explicit delete list but conceptually porep. See
  "Ambiguous / punted" below.

## index.html

Rewrote `web/static/index.html`:

- Removed `<script type="module">` tags for every deleted top-level `.mjs`.
- Removed `<cluster-machines>`, `<cluster-tasks>`, `<cluster-task-history>`,
  `<harmony-task-counts>`, `<pipeline-porep>`, `<pipeline-stats>` element
  instances from the dashboard body.
- Kept `<chain-connectivity>`, `<network-summary>`, `<storage-use>`,
  `<storage-gc-stats>`, `<actor-summary>`, `<win-stats>`.

## Nav (web/static/ux/curio-ux.mjs)

Removed nav `<li>` entries:

- Sectors (`/sector/`)
- PoRep (`/pages/pipeline_porep/`)
- Snap (`/snap/`)
- Market Settings (`/pages/market-settings/`)
- Snark Market (`/pages/proofshare/`)
- Wallets (`/pages/wallet/`)

Kept (and left untouched): Overview, Configurations, Storage Market, MK12, MK20,
IPNI, PDP, IPFS Content, Docs, Alerts indicator.

> Note: the strip task listed "Node Info, Task, Storage Paths, Piece, Upload
> Status, Actor" as nav links to keep. Those pages exist under `/pages/*/` but
> are NOT currently surfaced as left-rail nav entries in the upstream curio-ux
> menu (they're linked from other pages or hit directly). Did not add new nav
> entries for them — kept the existing menu structure minus the deletions.
> Flagging here in case Andy wants explicit top-level nav for those routes.

## Go-side changes

### web/srv.go
- Added `//go:build cgo`.
- Rewrote internal Curio web imports to use `github.com/Reiers/curio-core/web/...`
  instead of `github.com/filecoin-project/curio/web/...` (so our stripped copy
  is what gets compiled, not the upstream one).
- No route registrations were touched — `srv.go` is a generic file server, all
  static pages are served via the catch-all `NotFoundHandler`, no per-page
  routes.

### web/api/routes.go
- Dropped the `/api/sector` subrouter (it backed the deleted `/sector/` page).
- Deleted `web/api/sector/` entirely.
- Left a comment marker explaining the removal.

### web/api/webrpc/ (RPC handler files dropped)
The RPC handlers below were removed because they exclusively backed the
deleted (porep / clustering / sealing) UI surfaces:

```
pipeline_porep.go      — porep pipeline RPCs
pipeline_stats.go      — porep pipeline stats
cluster.go             — cluster machines/tasks RPCs
commr_check.go         — sealing CommR verification
unsealed_check.go      — sealing unsealed-sector check
sector.go              — sealing sector RPCs
wdpost_proving.go      — WindowPoSt (sealing) deadline/partition pages
proofshare.go          — porep proof outsourcing service (Snark Market)
upgrade.go             — imported tasks/snap (snap was deleted)
wallet.go              — backed the deleted wallet page
```

### web/api/webrpc/deals.go (stubbed)
- `DealsSealNow` now returns `xerrors.New("DealsSealNow disabled in
  curio-core (sealing pipeline removed)")` with a `TODO(curio-core)` comment.
  The `market/storageingest` import was dropped along with the unused `abi`
  import. `DealsPending` is unchanged. MK12 deals page can still load; only
  the "seal now" button (sealing pipeline) is disabled.

### KEPT webrpc files
```
actor_charts.go, actor_summary.go, alerts.go, balance_manager.go,
deals.go (stubbed), epoch.go, harmony_stats.go, ipfs_content.go,
ipni.go, market.go, market_2.go, market_filters.go, message.go,
net_summary.go, nulljson.go, pdp.go, piece.go, routes.go,
storage_path.go, storage_stats.go, sync_state.go, tasks.go, win_stats.go
```

All back kept pages or shared infrastructure (chain access, harmonytask, etc.).

## TODOs left in source

- `web/api/routes.go` — comment explaining `/api/sector` removal, with a
  re-add hint if a kept page ever needs sector-level APIs.
- `web/api/webrpc/deals.go` — `TODO(curio-core)` on `DealsSealNow`.

## Ambiguous / punted (NOT decided in this pass)

These are NOT on Andy's explicit delete list, NOT on his explicit keep list,
and are arguably sealing-era. Left in place to avoid over-stripping. Flag for
follow-up with Andy:

1. **`web/static/win-stats.mjs` + `<win-stats>` widget on the dashboard**
   ("Recent Wins"). This is WinningPoSt activity — sealing-era proving. The
   matching backend file `webrpc/win_stats.go` was kept. If Andy wants the
   PDP-only dashboard cleaner, drop both `win-stats.mjs` and `win_stats.go`
   plus the `<win-stats>` element in `index.html`.

2. **`web/static/pages/proofshare/`** (Snark Market). The page directory was
   NOT on the delete list, but the backend RPC (`webrpc/proofshare.go`) was
   porep-only and dragged in `curio/lib/proofsvc/common` which fails under
   pure-Go. The backend was dropped; the static pages will now return JSON-RPC
   errors when they try to call `ProofshareXxx` methods. Either drop the
   `pages/proofshare/` dir too, or re-add the backend if Andy wants Snark
   Market kept. Was already removed from the nav.

3. **`web/api/webrpc/balance_manager.go`** — backs a balance-manager UI that
   may or may not be sealing-coupled. Kept it (no obvious sealing import,
   imports clean under `//go:build cgo` gated package).

4. **`upgrade.go`** (deleted). It only contained the snap-upgrade RPC backing
   the deleted snap page. Confirmed by its `tasks/snap` import. If a non-snap
   "upgrade" feature ever rides on this filename, it'll need re-creation.

5. **Why-build-tag-on-everything**: longer term, the right fix is to detoxify
   `lotus/storage/paths` + `gosigar` + `curio/lib/ffi` under !cgo (matching the
   `tasks/pdp` carveout). When that happens, the `//go:build cgo` lines come
   out and `web/` builds pure-Go like the rest of curio-core.

## Verification

```
$ cd ~/.openclaw/workspace/projects/curio-core
$ CGO_ENABLED=0 go build ./...
(clean exit, no output)
$ CGO_ENABLED=0 go vet ./...
(clean exit, no output)
```
