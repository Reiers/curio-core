// Higher-level API matching the subset of curiostorage/harmonyquery.DB
// that Curio's harmonytask + tasks/pdp code paths exercise.
//
// Surface implemented here:
//   - Exec(ctx, sql, args...) (count int, err error)
//   - Select(ctx, sliceOfStructPtr, sql, args...) error
//   - BeginTransaction(ctx, fn, opts...) (didCommit bool, err error)
//
// Notable differences from upstream harmonyquery:
//   - upstream's `sql rawStringOnly` injection guard is dropped here in
//     favour of `string` for simplicity; curio-core's callers all pass
//     literal SQL strings, so the guard is paper-thin in our context.
//     Future re-introduction is straightforward when the package
//     surface stabilises.
//   - Select uses database/sql Rows.Scan + reflection for struct binding.
//     Faster than dbscan for fixed-shape queries; matches the upstream
//     behaviour for the common 'select one row into struct' case.

package harmonysqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Exec runs a non-query and returns the number of rows affected (matching
// upstream harmonyquery's signature shape).
func (d *DB) ExecCount(ctx context.Context, query string, args ...any) (int, error) {
	res, err := d.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return int(n), nil
}

// Select reads rows into either:
//   - a slice of struct pointers / slice of structs, via reflection +
//     `db:"..."` tag binding (mirrors harmonyquery.Select); or
//   - a slice of scalars (int64, TaskID-style typed int64, string, etc.)
//     for single-column SELECT/UPDATE...RETURNING queries.
//
// The scalar shape is what task_type_handler.go's task-accept claim path
// expects: `RETURNING id` into `*[]TaskID`. harmonyquery's pgx-backed
// Select handles both shapes via dbscan; we reproduce the same surface
// here for the hand-rolled reflector path.
func (d *DB) Select(ctx context.Context, sliceOfStructPtr any, query string, args ...any) error {
	rv := reflect.ValueOf(sliceOfStructPtr)
	if rv.Kind() != reflect.Ptr || rv.Elem().Kind() != reflect.Slice {
		return errors.New("Select: dest must be a pointer to a slice")
	}
	sliceVal := rv.Elem()
	elemType := sliceVal.Type().Elem()
	// elemType may itself be a pointer to struct, or struct.
	isPtrElem := elemType.Kind() == reflect.Ptr
	structType := elemType
	if isPtrElem {
		structType = elemType.Elem()
	}

	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	// Scalar-slice shape: dst is *[]T where T is a non-struct kind. We
	// require exactly one selected column.
	if structType.Kind() != reflect.Struct {
		if isPtrElem {
			return errors.New("Select: slice of pointer to scalar not supported; use *[]T instead of *[]*T")
		}
		if len(cols) != 1 {
			return fmt.Errorf("Select: scalar-slice destination requires exactly 1 column, got %d (%v)", len(cols), cols)
		}
		for rows.Next() {
			elem := reflect.New(elemType).Elem()
			if err := scanWithTimeFix(rows.Scan, elem.Addr().Interface()); err != nil {
				return fmt.Errorf("scan scalar row: %w", err)
			}
			sliceVal.Set(reflect.Append(sliceVal, elem))
		}
		return rows.Err()
	}

	// Pre-compute column-to-field map via db tags.
	fieldByCol := make(map[string]int, len(cols))
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}
		fieldByCol[strings.ToLower(tag)] = i
	}

	dests := make([]any, len(cols))
	for rows.Next() {
		elem := reflect.New(structType).Elem()
		for i, col := range cols {
			fi, ok := fieldByCol[strings.ToLower(col)]
			if !ok {
				// no matching tag — scan into a sink
				var sink any
				dests[i] = &sink
				continue
			}
			dests[i] = elem.Field(fi).Addr().Interface()
		}
		if err := scanWithTimeFix(rows.Scan, dests...); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		if isPtrElem {
			ptr := reflect.New(structType)
			ptr.Elem().Set(elem)
			sliceVal.Set(reflect.Append(sliceVal, ptr))
		} else {
			sliceVal.Set(reflect.Append(sliceVal, elem))
		}
	}
	return rows.Err()
}

// Tx wraps a database/sql transaction so callers see the same surface
// upstream harmonyquery provides.
type Tx struct {
	tx *sql.Tx
}

// Exec on a Tx.
func (t *Tx) Exec(query string, args ...any) (int, error) {
	res, err := t.tx.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return int(n), nil
}

// Query on a Tx.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.Query(query, args...)
}

// QueryRow on a Tx.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRow(query, args...)
}

// BeginTransaction runs fn inside a transaction. fn returns
// (commit bool, err error): commit=true triggers a COMMIT, false a
// ROLLBACK. Error propagates either way.
//
// Mirrors harmonyquery.BeginTransaction (minus the retry-on-serialization
// loop which is Postgres-specific; SQLite single-writer doesn't see
// serialization failures the same way).
func (d *DB) BeginTransaction(ctx context.Context, fn func(*Tx) (commit bool, err error)) (didCommit bool, retErr error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	commit, fnErr := fn(&Tx{tx: tx})
	if fnErr != nil {
		_ = tx.Rollback()
		return false, fnErr
	}
	if !commit {
		if err := tx.Rollback(); err != nil {
			return false, fmt.Errorf("rollback: %w", err)
		}
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}
