# Migration port status — Curio Core SQLite schema

The `schema-curio-core/` directory holds Curio Core's hand-translated
SQLite schema migrations. They're applied in lexical filename order by
`harmonysqlite.DB.ApplyMigrations(ctx)`, with idempotency tracked in
`harmony_schema_migrations`.

The schema is intentionally NOT a port of all 118 upstream Curio Postgres
migrations. We only translate what tasks/pdp + harmonytask actually need;
sealing-pipeline, mining, scrub, snap, market-non-PDP migrations are
explicitly out of scope per `docs/SCOPE-DIFF.md`.

## Shipped (6 files, harmonytask core)

| File | Source | Notes |
|---|---|---|
| 0001_harmony_core.sql | 20230719-harmony.sql | task system base |
| 0002_harmony_singleton_task.sql | 20240416-harmony_singleton_task.sql | singleton lock |
| 0003_machine_detail.sql | 20240404 + 20240527 | machine inventory |
| 0004_chain_sends.sql | 20231103-chain_sends.sql | message sends + locks |
| 0005_message_waits.sql | 20231225-message-waits.sql | message wait queue |
| 0006_common_layers.sql | 20240212-common-layers.sql | config layer storage |

## Outstanding (Day 4-5 followup)

The 28 PDP-domain migrations carry significant Postgres-specific machinery:

- **CREATE TRIGGER + CREATE FUNCTION** (in 20240930-pdp.sql and friends):
  reference-counting on `pdp_proofset_refcount` is currently done via
  PL/pgSQL triggers. SQLite has triggers but no PL/pgSQL functions. Two
  strategies on the table:
  - Translate trigger bodies into pure-SQLite TRIGGER syntax (works for
    the simpler ones).
  - Move the refcount logic out of the DB layer into Go application code
    that runs inside `BeginTransaction(...)` blocks. Cleaner, easier to
    test, compatible with Andy's three-layer harmonytask shape.
- **BYTEA**: BLOB equivalent (already done in 0004/0005).
- **UUID**: store as TEXT, validate in Go. Affects pdp_piece_uploads.id.
- **ALTER COLUMN SET NOT NULL / SET DEFAULT**: SQLite doesn't support
  these. Use the 12-step migration pattern (rename → create new → copy
  → drop → rename).
- **Postgres text[] columns**: store as JSON in TEXT.

The 28 PDP-domain migrations are the actual Day 4-5 sprint work and will
land as follow-up commits.
