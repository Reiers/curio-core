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

	"github.com/curiostorage/harmonyquery"
)

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

func (s *sqliteTxI) SelectI(sliceOfStructPtr any, sql harmonyquery.RawString, arguments ...any) error {
	// The existing Tx.Select API doesn't exist on harmonysqlite.Tx yet.
	// Use a query + scan loop directly. For now, return not-implemented;
	// PDP task code paths that hit Select-in-Tx will surface during the
	// Day 7 calibration run and we'll port the harmonyquery scany code
	// then.
	return errTxSelectNotImplemented
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

// errTxSelectNotImplemented marks call paths that Day 7 calibration must
// exercise. When a real PDP task does tx.Select inside a transaction,
// we'll see this error and port the scany-based row decoder here.
type txSelectNotImplementedT struct{}

func (txSelectNotImplementedT) Error() string {
	return "harmonysqlite: TxInterface.Select not yet ported (Day 7 follow-up)"
}

var errTxSelectNotImplemented error = txSelectNotImplementedT{}
