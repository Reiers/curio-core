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
	"strings"
	"time"

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

// QueryRowI implements harmonyquery.DBInterface.QueryRow. Wraps the
// underlying *sql.Row in rowFromSqlRow so per-column Scan calls route
// through scanWithTimeFix (handles modernc.org/sqlite's TEXT-shaped
// timestamps when the destination is *time.Time).
func (d *DB) QueryRowI(ctx context.Context, sql harmonyquery.RawString, arguments ...any) harmonyquery.Row {
	return rowFromSqlRow{r: d.QueryRow(ctx, string(sql), arguments...)}
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
	if err := dbscan.ScanAll(sliceOfStructPtr, &rowsWithTimeFix{Rows: rows}); err != nil {
		return fmt.Errorf("harmonysqlite Tx.SelectI: scan: %w", err)
	}
	return nil
}

// rowsWithTimeFix wraps *sql.Rows so dbscan's per-row Scan call routes
// through scanWithTimeFix, which handles modernc.org/sqlite's TEXT-shaped
// timestamp columns when the destination is *time.Time. Every other
// method (Close/Err/Next/Columns/NextResultSet) passes through unchanged.
//
// dbscan.Rows is a superset of *sql.Rows's public surface plus
// NextResultSet, which sql.Rows also provides since Go 1.8 — the
// embedded *sql.Rows satisfies the interface natively for those methods.
type rowsWithTimeFix struct {
	*sql.Rows
}

func (r *rowsWithTimeFix) Scan(dest ...any) error {
	return scanWithTimeFix(r.Rows.Scan, dest...)
}

// SendBatch is pgx-specific batch protocol. Not supported on SQLite.
// Callers (curio/market/ipni/chunker bulk inserts) aren't reached by
// curio-core's PDP-only deployment shape.
func (s *sqliteTxI) SendBatch(ctx context.Context, b *pgx.Batch) (pgx.BatchResults, error) {
	return nil, errNotSupportedOnSQLite("SendBatch")
}

// QueryI is the in-transaction streaming cursor. Implemented by wrapping
// *sql.Rows as a harmonyquery.Qry. Required for upstream code paths that
// iterate row-by-row inside a tx (e.g. pdp/handlers_add.go's subPieces
// validation under the IN-list shape we ported from ANY($2)).
func (s *sqliteTxI) QueryI(sqlStr harmonyquery.RawString, arguments ...any) (*harmonyquery.Query, error) {
	rows, err := s.tx.Query(string(sqlStr), arguments...)
	if err != nil {
		return nil, err
	}
	return &harmonyquery.Query{Qry: &sqliteQry{rows: rows}}, nil
}

// sqliteQry adapts *sql.Rows to harmonyquery.Qry. Six methods total:
// Next, Err, Close, Scan, Values, plus a couple of internal helpers.
// Scan goes through scanWithTimeFix so TEXT timestamps round-trip into
// time.Time correctly (same shim as SelectI).
type sqliteQry struct {
	rows *sql.Rows
}

func (q *sqliteQry) Next() bool            { return q.rows.Next() }
func (q *sqliteQry) Err() error            { return q.rows.Err() }
func (q *sqliteQry) Close()                { _ = q.rows.Close() }
func (q *sqliteQry) Scan(dst ...any) error { return scanWithTimeFix(q.rows.Scan, dst...) }
func (q *sqliteQry) Values() ([]any, error) {
	cols, err := q.rows.Columns()
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := q.rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return vals, nil
}

func (s *sqliteTxI) QueryRowI(sql harmonyquery.RawString, arguments ...any) harmonyquery.Row {
	return rowFromSqlRow{r: s.tx.QueryRow(string(sql), arguments...)}
}

// rowFromSqlRow adapts *sql.Row to harmonyquery.Row.
type rowFromSqlRow struct {
	r *sql.Row
}

func (r rowFromSqlRow) Scan(dst ...any) error {
	return scanWithTimeFix(r.r.Scan, dst...)
}

// scanWithTimeFix wraps a Scan() call to fix the modernc.org/sqlite
// issue where CURRENT_TIMESTAMP / TIMESTAMP TEXT columns can't be
// scanned directly into *time.Time. We pass a *string proxy for every
// *time.Time destination, then parse the SQLite-flavoured datetime
// string into the original *time.Time after the underlying Scan
// returns.
//
// Supported SQLite datetime formats (in priority order):
//   - "2006-01-02 15:04:05.999999999" (high-precision, modernc default)
//   - "2006-01-02 15:04:05"           (CURRENT_TIMESTAMP default)
//   - time.RFC3339Nano                 (Go's MarshalJSON shape)
//   - time.RFC3339
//
// NULL is mapped to a zero time.Time without error (matches pgx/yugabyte
// behaviour for NULL TIMESTAMP).
func scanWithTimeFix(scan func(...any) error, dst ...any) error {
	timeProxies := make(map[int]*string, 0)
	newDst := make([]any, len(dst))
	for i, d := range dst {
		if tp, ok := d.(*time.Time); ok && tp != nil {
			var s sql.NullString
			newDst[i] = &s
			timeProxies[i] = new(string)
			// Use a closure-captured proxy: after scan, parse s into *time.Time.
			_ = tp
			newDst[i] = &s
			timeProxies[i] = (*string)(nil) // sentinel; we'll resolve via newDst
			continue
		}
		newDst[i] = d
	}
	if err := scan(newDst...); err != nil {
		return err
	}
	for i := range dst {
		tp, ok := dst[i].(*time.Time)
		if !ok || tp == nil {
			continue
		}
		_ = timeProxies // silence linter; timeProxies kept for future expansion
		ns := newDst[i].(*sql.NullString)
		if !ns.Valid {
			*tp = time.Time{}
			continue
		}
		t, err := parseSQLiteTime(ns.String)
		if err != nil {
			return fmt.Errorf("scanWithTimeFix: column %d: parse %q as time: %w", i, ns.String, err)
		}
		*tp = t
	}
	return nil
}

// parseSQLiteTime tries the SQLite datetime formats modernc produces
// (and that CURRENT_TIMESTAMP defaults emit) before falling back to
// the RFC3339 family Go marshals time.Time as. It also recognizes the
// shape modernc.org/sqlite stores when a Go time.Time is passed as a
// driver.Value: Go's default time.Time.String() output, which looks
// like "2026-05-24 09:44:38.218610887 +0000 UTC" optionally with a
// monotonic clock suffix " m=+2.176855639".
//
// Provenance of each format:
//
//   - "2006-01-02 15:04:05.999999999 -0700 MST"  Go time.Time.String()
//     (modernc default Value())
//   - "2006-01-02 15:04:05.999999999"             modernc explicit fmt
//   - "2006-01-02 15:04:05"                       SQLite CURRENT_TIMESTAMP
//   - time.RFC3339Nano / time.RFC3339             Go MarshalJSON / -Text shape
func parseSQLiteTime(s string) (time.Time, error) {
	// Strip Go's monotonic-clock suffix when present. time.Time.String()
	// appends ` m=±<seconds>` whenever the value carries a monotonic
	// reading; time.Parse rejects that suffix.
	if i := strings.Index(s, " m=+"); i >= 0 {
		s = s[:i]
	} else if i := strings.Index(s, " m=-"); i >= 0 {
		s = s[:i]
	}
	layouts := []string{
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
		time.RFC3339,
	}
	var lastErr error
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

// (errTxSelectNotImplemented removed — Tx.SelectI is now real.)
