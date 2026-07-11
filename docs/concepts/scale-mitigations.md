# Scale mitigations: five upstream GA-blockers, absent by construction

Upstream [filecoin-project/curio](https://github.com/filecoin-project/curio) carries a set of
open GA-blockers on the hot-storage / PDP path that stem from its distributed design
(YugabyteDB tablets, full PieceGC, cleanup-pieces, IPNI storm). Curio Core does not ship
any of them, because the code that produces them was never carried over during the
carve-out.

This page enumerates the five, points at the code in Curio Core that makes each one
impossible, and names the structural test that guards against a silent regression.

> **What this page does not claim.** Curio Core has not yet run at the largest-cluster
> scale that Curio targets. It has run a 5,246-pieceref calibration soak since
> 2026-06-18 with zero panics, and the first mainnet PDP proof landed
> [2026-06-25 block 6136371](https://beryx.io/mainnet/txs/bafy2bzacediigzemq…).
> The claim on this page is _"by construction these five failure modes cannot occur
> in Curio Core"_, not _"proven at 10× Curio's scale."_

---

## The five

### 1. filecoin-project/curio#1310 - cleanupPieces / SaveCache full-scans on `pdp_piecerefs`

**Upstream root cause.** Yugabyte tablet contention on a 1.5M+ row full-scan of
`pdp_piecerefs`, plus missing partial indexes for the hot cleanup / scan queries.
At scale it saturates the DB.

**Why Curio Core is unaffected.**
Curio Core uses SQLite in single-writer mode
([`internal/harmonysqlite/db.go:80`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/db.go#L80),
`SetMaxOpenConns(1)`), so there is no tablet layer and no serialization contention.
Partial indexes for the hot paths are present:

- `idx_pdp_piecerefs_indexing_pending` on `pdp_piecerefs (created_at ASC) WHERE indexing_task_id IS NULL AND needs_indexing = 1` - migration
  [`0016_pdpv0_lifecycle_hardening.sql:58`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/schema-curio-core/0016_pdpv0_lifecycle_hardening.sql#L58)
- `idx_pdp_piece_uploads_notify` on `pdp_piece_uploads (created_at ASC) WHERE piece_ref IS NOT NULL AND notify_task_id IS NULL` -
  same migration line 104

### 2. #1303 - PieceGC 24h window destroys uploaded-but-uncommitted pieces

**Upstream root cause.** `PDPv0_PieceGC` reaps staged pieces on a fixed
24h window. If a client uploads and the commit lags, the piece is
garbage-collected before it can be used.

**Why Curio Core is unaffected.** `PDPv0_PieceGC` is not wired into the
task registry. See
[`internal/engine/engine.go` `BuildTaskRegistry()`](https://github.com/Reiers/curio-core/blob/main/internal/engine/engine.go#L555):
exactly nine PDPv0 tasks are registered (`Prove`, `PullPiece`, `SaveCache`,
`InitPP`, `ProvPeriod`, `Notify`, `DelDataSet`, `TermFWSS`, `ChainSync`).
`PDPv0_PieceGC` exists in
[`tasknames`](https://github.com/Reiers/curio-core/blob/main/tasknames)
but it never gets a task descriptor.

**Structural guard.** `TestRegistry_GABlockerTasksStayUnregistered` in
[`engine_test.go`](https://github.com/Reiers/curio-core/blob/main/internal/engine/engine_test.go#L168)
fails loudly if any future rebase silently re-registers `PDPv0_PieceGC`.

### 3. #1296 - un-includable cleanupPieces tx wedges sender nonce

**Upstream root cause.** A cleanupPieces tx that can't be included jams the
sender nonce queue and permanently breaks `ProvPeriod` after restart.

**Why Curio Core is unaffected.** The cleanup-pieces feature is not
present. There is no `PDPv0_Cleanup` task descriptor in the registry
([same `BuildTaskRegistry` as above](https://github.com/Reiers/curio-core/blob/main/internal/engine/engine.go#L555)),
and no cleanup-pieces path is wired at any lower layer.

**Structural guard.** Same `TestRegistry_GABlockerTasksStayUnregistered`
above: `PDPv0_Cleanup` is one of the three names it forbids.

### 4. #1291 - `PDPv0_IPNI Max(50)` serialization storm + stranded piecerefs

**Upstream root cause.** Under YugabyteDB's serializable isolation,
concurrent IPNI head writes throw `40001` serialization conflicts. When
IPNI tasks are dropped mid-flight, FK gaps leave piecerefs stranded
(unannounced, un-reapable).

**Why Curio Core is unaffected.**

- SQLite single-writer produces no `40001`-class errors, by construction.
- `PDPv0_IPNI` is not wired (see registry above). IPNI advertisement is
  tracked separately at
  [curio-core#42](https://github.com/Reiers/curio-core/issues/42).
- The stranded-pieceref half of the class is closed independently:
  every `harmony_task` FK column on `pdp_piecerefs` uses `ON DELETE SET
  NULL`, so a completed / deleted task cannot leave the pieceref pinned
  to a dead task id:
  - `pdp_piecerefs.save_cache_task_id` &rarr;
    [`0011_pdp_v0_dataset.sql:200`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/schema-curio-core/0011_pdp_v0_dataset.sql#L200)
  - `pdp_piecerefs.indexing_task_id` &rarr;
    [`0016_pdpv0_lifecycle_hardening.sql:42`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/schema-curio-core/0016_pdpv0_lifecycle_hardening.sql#L42)
  - `pdp_piecerefs.ipni_task_id` &rarr;
    [`0018_pdpv0_post_rename_catchup.sql:31`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/schema-curio-core/0018_pdpv0_post_rename_catchup.sql#L31)
  - `pdp_piece_uploads.notify_task_id` &rarr;
    [`0016_pdpv0_lifecycle_hardening.sql:87`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/schema-curio-core/0016_pdpv0_lifecycle_hardening.sql#L87)

**Structural guard.** Two tests:

- `TestRegistry_GABlockerTasksStayUnregistered` guards absence of
  `PDPv0_IPNI` in the task registry.
- `TestApplyMigrations_PiecerefsHarmonyTaskFKsAreSetNull` in
  [`migrations_test.go`](https://github.com/Reiers/curio-core/blob/main/internal/harmonysqlite/migrations_test.go)
  parses the applied schema and fails if any `harmony_task` FK column on
  `pdp_piecerefs` uses anything other than `ON DELETE SET NULL`.

### 5. #1282 - idle DB load from WebUI polling + save-cache scheduler scans

**Upstream root cause.** Duplicate web polling paths + scan-heavy scheduler
loops produce a measurable idle DB load on Yugabyte, cheaper to hide when
you have a cluster and expensive when you're a hot-storage SP.

**Why Curio Core is unaffected.**

- Same single-writer SQLite (no tablet load), so the "expensive on Yugabyte"
  cost class is gone.
- The upstream Curio WebUI is not vendored. Curio Core ships a new
  [`internal/dashboard`](https://github.com/Reiers/curio-core/tree/main/internal/dashboard)
  that binds loopback-only and drops the duplicate-poll paths that
  caused the idle load in the first place. See
  `internal-notes/WEBUI-STRIP-NOTES.md` (local, gitignored) for the strip
  audit trail, and
  [`docs/reference/upstream-diff.md`](/reference/upstream-diff.md) for
  the public summary.

---

## The structural guard, in one paragraph

Two Go tests keep this page honest:

- **`TestRegistry_GABlockerTasksStayUnregistered`** - asserts that
  `PDPv0_PieceGC`, `PDPv0_Cleanup`, and `PDPv0_IPNI` are absent from
  `BuildTaskRegistry()`. If a future upstream rebase silently
  re-registers any of them, the build fails.
- **`TestApplyMigrations_PiecerefsHarmonyTaskFKsAreSetNull`** -
  applies every migration to a fresh in-memory SQLite, then reads
  `PRAGMA foreign_key_list('pdp_piecerefs')` and fails if any FK
  targeting `harmony_task` uses anything other than `ON DELETE SET
  NULL`. Same shape check on `pdp_piece_uploads`.

Together they cover four of the five items on this page as CI checks.
Item 5 (idle DB load) is architectural (SQLite + stripped WebUI); the
upstream-diff doc is the durable reference for it.

## What we still need to earn

The claim on this page is bounded. What Curio Core has _not_ done:

- No demonstration under the largest-cluster load pattern Curio targets.
- No stress soak with 1.5M+ `pdp_piecerefs` (the row count that
  triggered upstream #1310).
- No adversarial test that fuzzes the task-completion &rarr; FK-cascade
  interaction on the piecerefs tables under concurrent add/delete.

Those are worthwhile follow-ups. The structural absence of the five
failure modes is a different thing than proof of scale, and it's the
only claim this page makes.
