// Package harmonysqlite — DBInterface adapter
//
// Implements harmonyquery.DBInterface + TxInterface on top of the existing
// *harmonysqlite.DB / *harmonysqlite.Tx implementations. The adapter
// methods have an "I" suffix or differ in signature from the existing
// stdlib-style methods, so both API shapes coexist.
//
// Why: curio-core's harmonytask refactor (Reiers/curio db-seam-refactor)
// changed the engine's internal DB handle from *harmonydb.DB to
// harmonyquery.DBInterface. This file is what makes the SQLite backend
// pluggable into the upstream harmonytask scheduler.

package harmonysqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/curiostorage/harmonyquery"
	"github.com/georgysavva/scany/v2/dbscan"
	"github.com/yugabyte/pgx/v5"
)

// errNotSupportedOnSQLite labels methods on harmonyquery.DBInterface /
// TxInterface that don't have a SQLite equivalent. Callers reaching
// these paths are exercising pgx-specific behaviour (batching, streaming
// cursors) that requires a Postgres backend.
func errNotSupportedOnSQLite(method string) error {
	return fmt.Errorf("harmonysqlite: %s not supported (pgx-specific; SQLite backend can't satisfy this contract). Curio Core PDP-only deployments don't exercise this code path.", method)
}

// Compile-time guards: if these break, the public surface drifted.
var _ harmonyquery.DBInterface = (*DB)(nil)
var _ harmonyquery.TxInterface = (*sqliteTxI)(nil)

// Exec implements harmonyquery.DBInterface.Exec by converting RawString
// to string and delegating to the existing ExecCount helper.
func (d *DB) ExecI(ctx context.Context, sql harmonyquery.RawString, arguments ...any) (int, error) {
	return d.ExecCount(ctx, string(sql), arguments...)
}

// SelectI implements harmonyquery.DBInterface.Select.
// The existing Select method already takes a `string` SQL parameter and
// scans into a sliceOfStructPtr, matching the contract.
func (d *DB) SelectI(ctx context.Context, sliceOfStructPtr any, sql harmonyquery.RawString, arguments ...any) error {
	return d.Select(ctx, sliceOfStructPtr, string(sql), arguments...)
}

// QueryRowI implements harmonyquery.DBInterface.QueryRow. Returns a
// harmonyquery.Row (interface with Scan(...any) error); *sql.Row
// satisfies that interface naturally via its Scan method.
func (d *DB) QueryRowI(ctx context.Context, sql harmonyquery.RawString, arguments ...any) harmonyquery.Row {
	return d.QueryRow(ctx, string(sql), arguments...)
}

// BeginTransactionI implements harmonyquery.DBInterface.BeginTransactionI.
// Wraps the existing BeginTransaction(*Tx) call by adapting the closure
// signature: the interface-typed callback gets a sqliteTxI wrapper around
// the concrete *Tx.
// QueryI returns a streaming cursor. SQLite doesn't expose harmonyquery's
// *Query type natively; we return a not-supported error because the
// upstream call sites that hit QueryI (ipni-provider's refreshProviders)
// aren't reached by curio-core's PDP-only deployment shape.
func (d *DB) QueryI(ctx context.Context, sql harmonyquery.RawString, arguments ...any) (*harmonyquery.Query, error) {
	return nil, errNotSupportedOnSQLite("QueryI")
}

func (d *DB) BeginTransactionI(ctx context.Context, fn func(harmonyquery.TxInterface) (commit bool, err error), opt ...harmonyquery.TransactionOption) (bool, error) {
	return d.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
		return fn(&sqliteTxI{tx: tx})
	})
}

// sqliteTxI wraps *Tx and presents harmonyquery.TxInterface to callers.
// The interface methods take RawString; the wrapped methods take string.
type sqliteTxI struct {
	tx *Tx
}

func (s *sqliteTxI) ExecI(sql harmonyquery.RawString, arguments ...any) (int, error) {
	return s.tx.Exec(string(sql), arguments...)
}

func (s *sqliteTxI) SelectI(sliceOfStructPtr any, q harmonyquery.RawString, arguments ...any) error {
	rows, err := s.tx.Query(string(q), arguments...)
	if err != nil {
		return fmt.Errorf("harmonysqlite Tx.SelectI: query: %w", err)
	}
	defer rows.Close()
	if err := dbscan.ScanAll(sliceOfStructPtr, rows); err != nil {
		return fmt.Errorf("harmonysqlite Tx.SelectI: scan: %w", err)
	}
	return nil
}

// SendBatch is pgx-specific batch protocol. Not supported on SQLite.
// Callers (curio/market/ipni/chunker bulk inserts) aren't reached by
// curio-core's PDP-only deployment shape.
func (s *sqliteTxI) SendBatch(ctx context.Context, b *pgx.Batch) (pgx.BatchResults, error) {
	return nil, errNotSupportedOnSQLite("SendBatch")
}

// QueryI is the in-transaction streaming cursor. SQLite doesn't expose
// harmonyquery's *Query type natively. Returns a not-supported error;
// callers reach this when they iterate row-by-row inside a tx (e.g.
// pdp/handlers_add.go's piece-add path under a SQL ANY() filter).
// Curio Core's PDP-only deployment shape doesn't exercise these paths
// in the demo flow.
func (s *sqliteTxI) QueryI(sql harmonyquery.RawString, arguments ...any) (*harmonyquery.Query, error) {
	return nil, errNotSupportedOnSQLite("TxInterface.QueryI")
}

func (s *sqliteTxI) QueryRowI(sql harmonyquery.RawString, arguments ...any) harmonyquery.Row {
	return rowFromSqlRow{r: s.tx.QueryRow(string(sql), arguments...)}
}

// rowFromSqlRow adapts *sql.Row to harmonyquery.Row.
type rowFromSqlRow struct {
	r *sql.Row
}

func (r rowFromSqlRow) Scan(dst ...any) error {
	return r.r.Scan(dst...)
}

// (errTxSelectNotImplemented removed — Tx.SelectI is now real.)
