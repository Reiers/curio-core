# Migration port status

## Shipped (6 files, harmonytask core)

These 6 migrations cover the load-bearing schema harmonytask + chain message
plumbing need. They are hand-translated from upstream Curio's Postgres
migrations and verified by `migrations_test.go`.

| File | Source | Notes |
|---|---|---|
| 0001_harmony_core.sql | 20230719-harmony.sql | task system base |
| 0002_harmony_singleton_task.sql | 20240416-harmony_singleton_task.sql | singleton lock |
| 0003_machine_detail.sql | 20240404 + 20240527 | machine inventory |
| 0004_chain_sends.sql | 20231103-chain_sends.sql | message sends + locks |
| 0005_message_waits.sql | 20231225-message-waits.sql | message wait queue |
| 0006_common_layers.sql | 20240212-common-layers.sql | config layer storage |

## Outstanding (Day 4-5 followup)

The 28 PDP-domain migrations carry significant Postgres-specific machinery
that needs careful translation:

- **CREATE TRIGGER + CREATE FUNCTION** (in 20240930-pdp.sql and friends):
  reference-counting on `pdp_proofset_refcount` is currently done via
  PL/pgSQL triggers. SQLite has triggers but **no PL/pgSQL functions**.
  Two strategies on the table:
  - Translate trigger bodies into pure-SQLite TRIGGER syntax (works for
    the simpler ones).
  - Move the refcount logic out of the DB layer into Go application code
    that runs inside `BeginTransaction(...)` blocks. Cleaner. Easier to
    test. Compatible with what Andy's `integ/task` refactor pushed (DB
    is one of three layers, not the whole thing).
- **BYTEA columns**: BLOB equivalent works. Already in 0004/0005.
- **UUID columns**: SQLite has no native UUID type. Store as TEXT, validate
  in Go. Affects `pdp_piece_uploads.id`.
- **ALTER COLUMN SET NOT NULL / SET DEFAULT**: not supported by SQLite.
  When upstream uses these, the SQLite port must rewrite the table via
  the standard 12-step migration pattern (rename → create new → copy
  → drop → rename).
- **Array columns** (text[], int[]): not in PDP-relevant migrations,
  but if encountered, store as JSON in TEXT and parse in Go.

The 28 PDP-domain migrations are the actual Day 4-5 sprint work and will
land as follow-up commits on the curio-core repo.

## Skipped permanently

The 65 sealing-pipeline + market-non-PDP migrations are out of scope.
See `docs/SCOPE-DIFF.md` for the full classification.
