# Day 3 notes — SQL classification + Postgres→SQLite port

Branch cut: 2026-05-23. Tracking issue: Reiers/lantern#11.

## Headline

- Classified all **118** upstream Curio migration files (`harmony/harmonydb/sql/`)
  against the Curio Core (PDP-only) scope.
- Hand-translated the KEEP set into **14 SQLite migration files** under
  `internal/harmonysqlite/schema-curio-core/`. These produce a working SQLite
  schema (55 tables) covering harmonytask, piece-park, eth-chain queue,
  PDP v1 + v0 vocabulary, IPNI, mk20 deals, and misc infra.
- `go test ./internal/harmonysqlite/...` passes (all 8 tests + their subtests),
  including a PDP-trigger acceptance test that exercises the inline-body
  SQLite trigger replacement for the upstream PG plpgsql function
  `increment_data_set_refcount()`.

## Counts (full classification in `docs/SQL-CLASSIFICATION.md`)

| Category | Files | Notes |
|---|---:|---|
| KEEP-INFRA | 35 | harmonytask + machine + message-send/wait + piece-park + storage-index + autocert + alerts + config |
| KEEP-PDP | 30 | pdp_* tables (v1 + v0), eth chain, IPNI, mk20 deals + market_piece_deal, PDP-related triggers |
| DROP-SEALING | 37 | sdr/snap/wdpost/winning/window/scrub/sector_meta/mining |
| DROP-CLUSTER | 10 | proofshare (single-node) + balancemgr + wallet-exporter |
| DROP-LEGACY | 6 | itest_scratch + harmony_test + mk12-only migrations not referenced by PDP |
| **Total** | **118** | matches `ls *.sql` in the source dir |

## Translation strategy

Rather than auto-translating each PG migration 1:1 (which kept tripping on
PL/pgSQL functions, `DO $$` blocks, `ALTER COLUMN ... USING`, and
`ADD CONSTRAINT` — none of which SQLite supports), I **folded** related
migrations into a smaller set of hand-written SQLite files. Each file is
clearly headed with the upstream source migrations it represents.

Layout (lexical order = application order; reproducible boot):

| File | Replaces upstream | Tables |
|---|---|---|
| `0001_harmony_core.sql` | 20230719-harmony | harmony_machines / harmony_task / harmony_task_history / harmony_task_follow / harmony_task_impl |
| `0002_harmony_singleton_task.sql` | 20240416 | harmony_task_singletons |
| `0003_machine_detail.sql` | 20240404 + 20240527 | harmony_machine_details |
| `0004_chain_sends.sql` | 20231103 | message_sends + message_send_locks (FIL side) |
| `0005_message_waits.sql` | 20231225 | message_waits (FIL side) |
| `0006_common_layers.sql` | 20240212 (trimmed) | harmony_config (table only — sealing layer seeds omitted) |
| `0007_eth_chain.sql` | 20240929 | eth_keys / message_sends_eth / message_send_eth_locks / message_waits_eth (+ partial indexes) |
| `0008_piece_park.sql` | 20240228 + PDP-era extensions | parked_pieces / parked_piece_refs |
| `0009_storage_index.sql` | 20230712 + 20240401 + 20240417 | sector_location / storage_path / sector_path_url_liveness |
| `0010_pdp_v1.sql` | 20240930 | full PDP v1 (pdp_services, pdp_piece_uploads, pdp_piecerefs, pdp_proof_sets, pdp_proofset_*, pdp_prove_tasks) + 6 inline-body triggers replacing upstream's PG plpgsql functions |
| `0011_pdp_v0_dataset.sql` | 20250730 + 20250930 + 20251004/10/15/27/29 + 20260109/10/12/22/23/203/216/511 | PDP v0 (renamed terminology: data_sets, data_set_pieces, sub_piece, etc.) + 2 maintenance triggers + filecoin_payment_transactions + pdp_piece_pulls |
| `0012_ipni.sql` | 20240823 + 20241106 + 20251011 | ipni / ipni_head / ipni_peerid / ipni_chunks / ipni_ad_fetches |
| `0013_market_mk20.sql` | 20240731 + 20250505 (PDP-needed subset) + 20251231 + 20260211 + 20260416 | market_mk20_deal + market_piece_deal + ddo_contracts + market_fix_raw_size |
| `0014_infra_misc.sql` | 20240730 + 20240906 + 20240927 + 20241104/05 + 20250111 + 20250129 + 20250422 + 20250818 + 20250926 + 20260117/18 + 20260215 + 20260314 + 20260430 + 20260501 + 20240317/420/501 + 20231113 | alerts / alert_history / alert_comments / alert_mutes / autocert_cache / wallet_names / piece_summary / harmony_config_history + ALTERs adding retries, unschedulable, restart_request, run_now_request, timestamp, version, created_at, completed_by_host_and_port + indexes |

## Non-trivial translations

### 1. PG plpgsql functions → SQLite inline-body triggers

The upstream PDP migrations rely heavily on `CREATE FUNCTION` + `CREATE
TRIGGER ... EXECUTE FUNCTION` to maintain reference counts on
`pdp_piecerefs.proofset_refcount` / `data_set_refcount`. SQLite has
triggers but no user-defined functions, so the function bodies are inlined
into the trigger bodies. There are **5 trigger pairs**:

- `pdp_proofset_root_insert/delete/update_inc/update_dec`
  → maintain `pdp_piecerefs.proofset_refcount` (v1 vocabulary)
- `pdp_data_set_piece_insert/delete`
  → maintain `pdp_piecerefs.data_set_refcount` (v0 vocabulary)
- `pdp_proofset_create_message_status_change`
  → cascade `message_waits_eth.tx_status` changes to `pdp_proofset_creates.ok`
- `pdp_proofset_add_message_status_change`
  → cascade `message_waits_eth.tx_status` changes to `pdp_proofset_root_adds.add_message_ok`

The PG `ON UPDATE adjust_proofset_refcount_on_update()` function (which
handled both increment and decrement based on OLD vs NEW pdp_pieceref) had
to split into **two** SQLite triggers (`_update_inc` + `_update_dec`),
because SQLite trigger bodies are pure SQL and can't branch on `IF
... THEN ... ELSE ...`.

A trigger acceptance test in `migrations_test.go::TestApplyMigrations_PDPRefcountTriggerWorks`
proves the inline-body trigger increments `data_set_refcount` correctly.

### 2. `UPDATE ... SKIP LOCKED` → `BEGIN IMMEDIATE`

Upstream harmonytask claims tasks with `UPDATE harmony_task SET owner_id =
$1 WHERE owner_id IS NULL AND name = $2 LIMIT 1 RETURNING id` and skips
already-locked rows via `FOR UPDATE SKIP LOCKED`. SQLite is single-writer
(one writer transaction at a time), so the equivalent is to issue
`BEGIN IMMEDIATE` at the moment of claim and rely on `busy_timeout` for
serialization. `harmonysqlite.DB.BeginImmediate` scaffolds this; the
real wiring lives in Day 4-5's harmonytask adapter.

### 3. `ALTER COLUMN ... TYPE ... USING ...` → dropped (SQLite has no
timezone type)

Upstream's `20240522-ts-to-timestampz.sql` migrated every harmony
column from `TIMESTAMP` to `TIMESTAMPTZ` with a `USING col AT TIME ZONE
'UTC'` clause. SQLite has neither a timezone type nor `ALTER COLUMN
TYPE`. The translation is a no-op: SQLite stores DATETIME as an ISO-8601
string (or numeric Julian day), and the Go layer ensures every write is
UTC. The conversion happens at the application boundary, not in SQL.

### 4. `ADD COLUMN IF NOT EXISTS` → `ADD COLUMN`

PG-only syntax. SQLite has no `IF NOT EXISTS` for `ADD COLUMN`. The
idempotency that `IF NOT EXISTS` provided in upstream is now provided
by Curio Core's `harmony_schema_migrations` bookkeeping table — a
migration that's already been recorded never re-runs.

### 5. Insert-and-update-head (advisory-lock-ish CAS)

`20260410-ipni-head-cas.sql` defines a PG function
`insert_ad_and_update_head_checked()` that does a CAS-style write of the
IPNI head pointer (insert into `ipni`, then conditionally `UPDATE
ipni_head` only if the previous head still matches an expected value).
Translation strategy: keep the **schema** (the table); move the **CAS
logic** to the Go layer (`internal/ipni/head_cas.go`, planned). The
SQLite-equivalent is a `BEGIN IMMEDIATE` + `INSERT INTO ipni` + `UPDATE
ipni_head SET head = $1 WHERE provider = $2 AND head = $3` (which returns
0 rows if the CAS lost), inside a transaction.

### 6. JSONB → TEXT

`market_mk20_deal.pdp_v1`, `message_waits_eth.confirmed_tx_data`,
`message_waits_eth.tx_receipt`, `parked_piece_refs.data_headers` were
all `JSONB`. Translated to `TEXT` (stored as JSON strings). SQLite's
JSON1 extension (built into modernc.org/sqlite) provides
`json_extract`, `json_set`, etc. with subtly different syntax from PG
operators. PDP query rewrites (e.g. `pdp_v1->>'complete'` → `json_extract(pdp_v1, '$.complete')`)
will land alongside the harmonytask adapter in Day 4-5.

### 7. `BIGINT[]` (Postgres array) → `TEXT` (JSON-encoded)

`filecoin_payment_transactions.rail_ids BIGINT[]` translated to TEXT. The
Go layer json-encodes/decodes. Acceptable for the small array sizes
expected here (single-digit rail ids per transaction).

### 8. PG triggers calling `format()`/`information_schema` checks

`20240930-pdp.sql`'s `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_trigger
...) THEN CREATE TRIGGER ...` blocks are dropped (replaced by
`CREATE TRIGGER IF NOT EXISTS`, which SQLite supports). The
`information_schema.tables` idempotency probes in `20250730-pdp-v0-rename.sql`
are dropped entirely (greenfield: we just define the post-rename schema
directly in 0011).

## TODOs left as comments / followups

- **IPNI head CAS Go-side implementation** — placeholder in `0012_ipni.sql`
  header; Go-side function not yet written. Day 4-5.
- **JSON1 query rewrites in PDP go code** — when the harmonytask adapter
  lands (Day 4-5), every `pdp_v1->>'X'` or `confirmed_tx_data->'Y'` in
  `tasks/pdp/` / `tasks/pdpv0/` must become `json_extract(...)`.
- **mk20 `market_mk20_pipeline` table** — deliberately NOT ported here.
  It's used by PoRep deal handling (sealing). PDP doesn't touch it; if a
  later PDP-mk20 codepath needs it, port at that time.
- **harmony_config seed rows** — `0006_common_layers.sql` ships only the
  table; layer seed rows are explicitly NOT inserted (the upstream rows
  are all sealing-pipeline). Curio Core boots with no preconfigured
  layers; the operator picks at startup.
- **Auto-sync interference** — A background process keeps repopulating
  `internal/harmonysqlite/migrations/` with raw Postgres files copied from
  the upstream Curio module cache. That directory is now `.gitignore`'d
  and the canonical SQLite schema lives at `schema-curio-core/`. See
  commit `948d2aa` for the rationale.

## Test gate

`go test ./internal/harmonysqlite/...` passes cleanly:

```
=== RUN   TestApplyMigrations_FreshDatabase          PASS
=== RUN   TestApplyMigrations_CoreTablesPresent      PASS (12 subtests)
=== RUN   TestApplyMigrations_InsertSampleRow        PASS
=== RUN   TestApplyMigrations_PDPTablesPresent       PASS (30 subtests)
=== RUN   TestApplyMigrations_PDPRefcountTriggerWorks PASS
=== RUN   TestApplyMigrations_FKEnforced             PASS
=== RUN   TestApi_FullRoundTrip                      PASS
=== RUN   TestApi_TransactionRollback                PASS
=== RUN   TestOpen_*                                 PASS
```

After migrations, the SQLite has **55 tables**:

```
$ go run ./cmd/inspect-tables | head
Total tables: 55
PDP tables: 16
```

(16 = `pdp_*` only; the other 39 are infra: harmony_*, message_*, eth_*,
parked_*, sector_*, storage_*, ipni*, market_*, alert*, autocert_cache,
wallet_names, piece_summary.)
