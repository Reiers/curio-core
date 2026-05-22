# Migration port status — Curio Core SQLite schema

The `schema-curio-core/` directory holds Curio Core's hand-translated
SQLite schema migrations. They're applied in lexical filename order by
`harmonysqlite.DB.ApplyMigrations(ctx)`, with idempotency tracked in
`harmony_schema_migrations`.

The schema is intentionally NOT a port of all 118 upstream Curio Postgres
migrations. We only translate what tasks/pdp + harmonytask actually need;
sealing-pipeline, mining, scrub, snap, market-non-PDP migrations are
explicitly out of scope per `../../../docs/SCOPE-DIFF.md` and the
classification in `../../../docs/SQL-CLASSIFICATION.md`.

## Shipped (14 files, 55 SQLite tables)

| File | Upstream source(s) | What it adds |
|---|---|---|
| 0001_harmony_core.sql | 20230719 | harmony_machines / harmony_task / harmony_task_history / harmony_task_follow / harmony_task_impl |
| 0002_harmony_singleton_task.sql | 20240416 | harmony_task_singletons |
| 0003_machine_detail.sql | 20240404 + 20240527 | harmony_machine_details |
| 0004_chain_sends.sql | 20231103 | FIL message_sends + message_send_locks |
| 0005_message_waits.sql | 20231225 | FIL message_waits |
| 0006_common_layers.sql | 20240212 (trimmed) | harmony_config (table only; sealing seeds dropped) |
| 0007_eth_chain.sql | 20240929 | eth_keys + message_sends_eth + message_send_eth_locks + message_waits_eth |
| 0008_piece_park.sql | 20240228 + 20240930 + 20250505 + 20251014 | parked_pieces + parked_piece_refs + indexes |
| 0009_storage_index.sql | 20230712 + 20240401 + 20240417 | sector_location + storage_path + sector_path_url_liveness |
| 0010_pdp_v1.sql | 20240930 + 20250113 + 20250603 | PDP v1 (9 tables + 6 inline-body triggers) |
| 0011_pdp_v0_dataset.sql | 20250730 + 20250930 + 20251004/10/15/27/29 + 20260109/10/12/22/23/203/216/511 | PDP v0 vocabulary (7 tables + 2 triggers + folded ALTERs) |
| 0012_ipni.sql | 20240823 + 20241106 + 20251011 | ipni + ipni_head + ipni_peerid + ipni_chunks + ipni_ad_fetches |
| 0013_market_mk20.sql | 20240731 + 20250505 + 20251231 + 20260211 + 20260416 | market_mk20_deal + market_piece_deal + ddo_contracts + market_fix_raw_size |
| 0014_infra_misc.sql | 20240730 + 20240906 + 20240927 + 20241104/05 + 20250111 + 20250129 + 20250422 + 20250818 + 20250926 + 20260117/18 + 20260215 + 20260314 + 20260430 + 20260501 + harmony_task_history additions | alerts + alert_history + alert_comments + alert_mutes + autocert_cache + wallet_names + piece_summary + harmony_config_history + late ALTERs + indexes |

## Test gate

```
$ go test ./internal/harmonysqlite/...
ok  	github.com/Reiers/curio-core/internal/harmonysqlite	0.3s
```

All 8 tests (and ~50 subtests) pass, including the PDP-refcount trigger
acceptance test that verifies the inline-body SQLite triggers behave
the same as the upstream PG plpgsql functions they replace.

## Out of scope (deferred to Day 4-5)

- **Query rewrites in tasks/pdp/** — every `pdp_v1->>'X'` /
  `confirmed_tx_data->'Y'` operator that PDP code uses must become
  `json_extract(...)` to satisfy SQLite's JSON1 extension. This is a
  Go-side change, not a schema change.
- **IPNI head CAS Go-side** — the upstream PG function
  `insert_ad_and_update_head_checked()` is reimplemented at the Go
  layer; the schema is already in place (`ipni_head` table).
- **harmonytask DB adapter** — needs to satisfy the subset of
  `harmonydb.DB` that upstream Curio's `harmonytask` package consumes.
  Scaffold in `internal/harmonysqlite/api.go`; full surface (BeginTransaction
  + Select + Exec + RetryDBN) lands Day 4-5.

## Auto-sync workaround

The sibling directory `../migrations/` is `.gitignore`'d. A background
process (likely a Curio module-cache mirror) keeps repopulating it with
raw Postgres files; we route around it by keeping our canonical
SQLite schema here under a different name. See commit `948d2aa` for full
context.
