# The harmonytask scheduler

Curio Core inherits the upstream Curio task scheduler verbatim — only the underlying
DB is different (SQLite instead of Yugabyte). If you know upstream Curio's harmonytask,
nothing here will surprise you.

## What is a task?

A **task** is one row in the `harmony_task` table with a name, a posted time, and
optionally an `owner_id` (the engine instance that claimed it). When a task is picked
up by an engine, the engine runs the registered `Do(ctx, taskID, stillOwned)` Go
function, then writes the outcome into `harmony_task_history`.

## What runs in curio-core?

| Task name | Source | Role |
|---|---|---|
| `PDPv0_Prove`     | Upstream curio | Generates + broadcasts a possession proof for a dataset |
| `PDPv0_InitPP`    | Upstream curio | Advances `nextProvingPeriod` after a successful prove |
| `PDPv0_SaveCache` | Upstream curio | Pre-computes Merkle trees |
| `PDPv0_PullPiece` | Upstream curio | Downloads a piece from an external URL when a client requests pull-from-URL |
| `PDPv0_Notify`    | Upstream curio | HTTP callback after upload completes |
| `PDPv0_ChainSync` | Upstream curio | Reconciles dataset deletion state from chain events |
| `PDPv0_PaySettle` | **curio-core** | USDFC rail discovery + `settleRail` dispatch (10-min singleton) |
| `ParkComplete`    | **curio-core** | Flips `parked_pieces.complete=1` when streaming-upload bytes land |
| `SendTransaction` | Upstream curio | Generic SenderETH dispatcher (drains the tx send queue) |

## Scheduling model

The harmonytask scheduler is **registration-order priority**: it walks every registered
task type in the order they were registered, and for each type tries to claim the
oldest unowned row that satisfies the type's resource budget.

Within a type, tasks are picked by `ORDER BY update_time ASC`.

There is no cross-type priority. If `PDPv0_Prove` is contending with
`PDPv0_PullPiece` for the same CPU/RAM, the one registered first wins.

## Resource budget

Each task type declares a `Cost` (CPU/RAM/GPU). The engine has a global resource
budget (`--engine-cpu`, `--engine-ram` flags; defaults 4 CPU / 4 GiB). A task only runs
when the engine has the resources to take it.

Configuration:

```bash
curio-core run \
  --engine-cpu 4 \
  --engine-ram 4GiB
```

For a laptop SP, 4 CPU / 4 GiB is plenty. For an SP that handles heavy concurrent
client uploads, bump RAM to 8 GiB+.

## Diagnosing stuck tasks

The dashboard's **Tasks** page shows current active tasks + the last 50 history rows.
For deeper diagnosis:

```sql
-- recent failures with their first error line
SELECT id, name, substr(work_end, 1, 19) AS ended, substr(err, 1, 100) AS err
FROM harmony_task_history
WHERE result = 0 AND work_end >= datetime('now', '-24 hours')
ORDER BY id DESC LIMIT 20;

-- queued but unowned (work piling up?)
SELECT name, COUNT(*) AS waiting
FROM harmony_task
WHERE owner_id IS NULL
GROUP BY name;
```

If you see a task sitting unowned for minutes while the engine looks idle, the most
likely cause is **resource budget**: the task's `Cost.RAM` exceeds the engine's budget.
This used to be silent — curio-core ships a `WARN` log line when it happens (see
upstream PR #1245 / [curio-core#24](https://github.com/Reiers/curio-core/issues/24)).

## Force-running a singleton

Singleton tasks (`PDPv0_PaySettle`, `PDPv0_ChainSync`, `ParkComplete`) run on an
IAmBored cadence. To kick one off manually:

```sql
UPDATE harmony_task_singletons
SET run_now_request = 1
WHERE task_name = 'PDPv0_PaySettle';
```

Within ~30 seconds the singleton scheduler picks up the flag and re-runs the task.
