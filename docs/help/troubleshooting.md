# Troubleshooting

Common issues and what to check, in rough order of likelihood.

## Chain head isn't ticking

**Symptom:** Dashboard Overview shows the chain head frozen for more than ~1 minute.

**Check:**

```bash
sudo journalctl -u curio-core -f | grep -E "lantern|headerstore|F3"
```

If you see "header sync stalled" or DRAND beacon errors, Lantern lost contact with
its libp2p peers. Usually self-heals within a few minutes.

If chronic: restart the daemon, which forces fresh peer discovery:

```bash
sudo systemctl restart curio-core
```

If still chronic, your network may be blocking libp2p TCP. Check `--vm-bridge-rpc`
points at a reachable upstream gateway.

## Proofs are failing

**Symptom:** Dashboard Overview shows non-zero "failed" count for the last 24h, or
the dataset shows `N consecutive fail`.

**Check the error column on the Tasks page**, or:

```sql
SELECT id, substr(work_end, 1, 19), substr(err, 1, 100)
FROM harmony_task_history
WHERE name = 'PDPv0_Prove' AND result = 0
ORDER BY id DESC LIMIT 5;
```

Common causes:

| Error contains | Cause | Fix |
|---|---|---|
| `insufficient funds` | PDP wallet out of gas | Top up tFIL/FIL |
| `failed to get chain randomness` | Lantern behind real chain | Wait or restart |
| `no subpiece found` | Piece bytes missing from stash | Restore from stash backup |
| `at max already` | Resource budget too tight | Bump `--engine-cpu` / `--engine-ram` |

## Task is queued but never runs

**Symptom:** Tasks page shows an unowned task that's been sitting for several
minutes while other tasks complete.

**Cause 1:** Task's declared `Cost.RAM` exceeds the engine budget.

> If *all* new tasks (not just heavy ones) stop being claimed ~10 minutes after
> boot and you see `FOREIGN KEY constraint failed`, that's a different bug — see
> [New work stops dispatching ~10 minutes after boot](#new-work-stops-dispatching-10-minutes-after-boot).

**Check the log** — curio-core emits a `WARN` line:

```
task waiting but machine cannot accept it
  task_type=PDPv0_Prove
  reason="Did not accept PDPv0_Prove task: out of RAM: required 3221225472 available 1073741824"
```

**Fix:**

```bash
sudo systemctl edit curio-core
# Change ExecStart to include: --engine-ram 8GiB
sudo systemctl restart curio-core
```

## New work stops dispatching ~10 minutes after boot

**Symptom:** The daemon starts fine and keeps proving existing datasets, but any
**new** action silently stalls after the node has been up for ~10 minutes. A
`createDataSet` hangs at `POST /pdp/data-sets`; new uploads sit unprocessed. The
log fills with:

```
ERROR harmonytask task_type_handler.go ... constraint failed: FOREIGN KEY constraint failed
ERROR harmonytask scheduler.go ... failed to check node flags  {"error": "sql: no rows in result set"}
```

**Cause:** The scheduler periodically reaps `harmony_machines` rows whose
`last_contact` is older than 10 minutes (`LOOKS_DEAD_TIMEOUT`). On affected builds
the per-node keepalive that refreshes `last_contact` never ran, so the node's own
machine row got reaped out from under the `harmony_task.owner_id` foreign key —
after which no task can be claimed.

**Check:**

```sql
SELECT id, host_and_port, last_contact FROM harmony_machines;
```

If this returns **zero rows** on a running daemon, you have this bug.

**Fix:** Upgrade to a build that includes the engine keepalive (curio-core #77).
A `systemctl restart curio-core` restores service immediately on a fixed binary;
on an unfixed binary it only buys another ~10 minutes.

## settleRail always reverts with `CannotSettleFutureEpochs`

**Symptom:** Dashboard Rails page shows `last attempt err` on every rail; logs show
`CannotSettleFutureEpochs(railId, max, attempted)` reverts.

**Cause:** This is **expected** during the 7-day grace window. The contract caps
`untilEpoch` based on the payer's account-side `lockupLastSettledAt`, which advances
slower than chain head. The `PDPv0_PaySettle` eligibility heuristic only attempts a
settle after the grace window passes.

**Action:** None. The loop will retry on its 10-minute cycle and eventually land
successful settles.

If you want to force a settle attempt for diagnosis (testing only):

```bash
sudo systemctl edit curio-core
# Add: Environment="CURIO_CORE_PAYMENTS_MIN_SETTLE_EPOCHS=120"
sudo systemctl restart curio-core
```

Set back to unset for production.

## Wallet balance not showing

**Symptom:** Dashboard Wallets page shows `—` instead of a number for tFIL or USDFC.

**Cause:** The eth client (embedded Lantern) is wired but the balance read failed.
Typically transient — eth_call to the upstream RPC timed out.

**Check:**

```bash
sudo journalctl -u curio-core -f | grep -E "BalanceAt|CallContract"
```

If chronic, your `--vm-bridge-rpc` may be unreachable or rate-limited.

## Dashboard returns 303 to `/setup` on every request

**Symptom:** Every URL redirects to `/setup` and `harmony_config` has no rows.

**Cause:** Older builds before the `setupweb.DisableFirstRunRedirect` flag had a
stricter setup-or-die middleware. Newer builds let you skip the wizard entirely.

**Fix:** Upgrade to the latest curio-core binary, or fill in the wizard at `/setup`.

## State DB corruption

**Symptom:** Daemon fails to start with `database disk image is malformed` or
`malformed database schema`.

**Cause:** Crash during a write, or filesystem-level corruption.

**Action:**

```bash
sudo systemctl stop curio-core

# Restore from latest backup
cp /backup/state-most-recent.sqlite /var/lib/curio-core/state.sqlite

# Run integrity check on the restored DB
sqlite3 /var/lib/curio-core/state.sqlite 'PRAGMA integrity_check'

sudo systemctl start curio-core
```

If you don't have a backup: see [Disaster recovery](/operating/backup#disaster-recovery-for-the-pdp-wallet).
The PDP wallet (the only truly irreplaceable thing) can be re-imported from your
offline export; everything else is recoverable from chain state on restart.

## Still stuck

Open an issue with logs: <https://github.com/Reiers/curio-core/issues>. Useful info to
include:

- `curio-core version`
- The error message or symptom
- `sudo journalctl -u curio-core --since "10 minutes ago"` (lightly redacted)
- Network (calibration vs mainnet)
- Whether the issue is reproducible
