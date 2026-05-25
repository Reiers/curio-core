# SQLite schema

The full schema lives under `internal/harmonysqlite/schema-curio-core/NNNN_*.sql` in
the repo. The daemon applies each migration in numeric order on startup and records
applied migrations in `harmony_schema_migrations`.

## Key tables

| Table | Purpose |
|---|---|
| `harmony_task` | Live task queue |
| `harmony_task_history` | Completed/failed task records |
| `harmony_task_singletons` | Singleton task state (`PDPv0_PaySettle`, `ParkComplete`, etc.) |
| `harmony_config` | TOML configuration bundle (one row, "base") |
| `harmony_schema_migrations` | Tracker of applied migrations |
| `eth_keys` | Secp256k1 private keys + role labels |
| `message_sends_eth` | Pending + sent FEVM transactions |
| `message_waits_eth` | Tx receipt tracking (pending → confirmed) |
| `pdp_data_sets` | Active client datasets |
| `pdp_data_set_pieces` | Pieces within datasets |
| `pdp_payment_rails` | Discovered FilecoinPay rails for this SP (curio-core-specific) |
| `pdp_piece_pulls` | Pull-from-URL upload sessions |
| `pdp_piece_pull_items` | Per-piece state within a pull session |
| `pdp_piece_streaming_uploads` | Streaming-upload sessions |
| `parked_pieces` | Local pieces backing dataset entries |
| `parked_piece_refs` | URL-to-bytes references for parked pieces |
| `pdp_prove_tasks` | Active prove-task bookkeeping |
| `pdp_services` | Registered services (FWSS endpoints) |
| `alerts` | Operator alerts feed |
| `ipni_head` | IPNI announcement tracker (placeholder) |

## Inspection

```bash
sqlite3 /var/lib/curio-core/state.sqlite '.tables'
sqlite3 /var/lib/curio-core/state.sqlite '.schema pdp_data_sets'
```

Or use the dashboard's **Terminal** to run `config show` and other read-only diagnostics.

## Direct queries

Curio Core stores all metadata in one SQLite file, which makes it easy to inspect:

```bash
# Count active datasets
sqlite3 state.sqlite "SELECT COUNT(*) FROM pdp_data_sets WHERE COALESCE(terminated_at_epoch, 0) = 0"

# Show last 10 prove tasks
sqlite3 -header -column state.sqlite "
  SELECT id, name, substr(work_end, 1, 19) AS ended, result, substr(COALESCE(err, ''), 1, 60) AS err
  FROM harmony_task_history
  WHERE name = 'PDPv0_Prove'
  ORDER BY id DESC LIMIT 10
"

# Total USDFC accumulated (read from chain, not from DB - this is a stand-in)
sqlite3 -header -column state.sqlite "
  SELECT rail_id, last_settled_epoch, last_settle_tx_hash
  FROM pdp_payment_rails
  WHERE terminated = 0
  ORDER BY rail_id
"
```

## Migrations

To add a new migration:

1. Create `internal/harmonysqlite/schema-curio-core/NNNN_<name>.sql` where `NNNN` is the
   next sequential number.
2. Write the SQL. Forward-only — no transactions wrapping multiple statements (SQLite
   bails on the second-DDL-after-DML pattern that Postgres handles transparently).
3. Add a corresponding entry to `PORT-STATUS.md` documenting which upstream migration
   this mirrors (if any).

On the next `curio-core run` start, the migration runner picks it up and records it in
`harmony_schema_migrations`. There is no `downgrade/` SQL by design (see
[Upgrading](/operating/upgrading) for the rollback model).
