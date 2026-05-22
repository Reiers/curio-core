// Package harmonysqlite is Curio Core's SQLite-backed replacement for
// github.com/curiostorage/harmonyquery (the Postgres-backed harmonydb
// adapter used by Curio's harmonytask).
//
// Why this package exists:
//
// Curio's harmonydb wraps harmonyquery, which is hardcoded to
// yugabyte/pgx/v5 and Postgres semantics. Curio Core wants pure-Go
// single-binary operation, no Postgres, no Yugabyte. modernc.org/sqlite
// is a pure-Go SQLite port (transpiled from C, no CGo) and gives us
// the single-server constraint that's correct for the PDP-only
// operator profile.
//
// Status (2026-05-23): scaffold. The package defines the connection
// surface and proves modernc.org/sqlite works end-to-end (open, BEGIN
// IMMEDIATE, UPDATE, COMMIT). It does NOT yet implement the full
// harmonyquery.DB API. That's the Day 4-5 sprint work.
//
// API target: provide a DB type that satisfies a subset of the
// harmonyquery.DB surface harmonytask needs (Query, QueryRow, Exec,
// BeginTransaction, plus schema migration). Postgres-specific features
// — advisory locks, SKIP LOCKED on UPDATE, JSONB operators, ARRAY
// types, ON CONFLICT DO UPDATE specifics — get translated to SQLite
// equivalents (BEGIN IMMEDIATE for SKIP LOCKED semantics, JSON1
// extension for JSONB, regular text for ARRAY, INSERT ON CONFLICT).
//
// Non-goals:
//
//   - Multi-server clustering. Curio Core is single-server by design.
//   - Drop-in compatibility with every harmonyquery feature.
//     Curio-Core-specific use cases only.
//   - Migration of Curio's full 118-file schema. Only PDP-relevant
//     migrations (~15-25 files) ported. See ../../docs/SCOPE-DIFF.md.
package harmonysqlite
