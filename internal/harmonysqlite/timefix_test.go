package harmonysqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/curiostorage/harmonyquery"
)

// timeRow mirrors the shape of upstream curio's harmony_task scan target
// (harmonytask.go:401-402 and scheduler.go:427-428): two time.Time fields
// tagged db:"posted_time" + db:"update_time", which dbscan.ScanAll
// reflects into.
//
// Pre-fix this test reproduces the curio-core#17 error:
//
//	scan row: sql: Scan error on column index 2, name "update_time":
//	unsupported Scan, storing driver.Value type string into type *time.Time
//
// Post-fix the test passes because both Select paths (the hand-rolled
// reflector in (*DB).Select and the dbscan-backed (*sqliteTxI).SelectI)
// route per-column rows.Scan through scanWithTimeFix.
type timeRow struct {
	ID         int64     `db:"id"`
	Name       string    `db:"name"`
	UpdateTime time.Time `db:"update_time"`
	PostedTime time.Time `db:"posted_time"`
}

func TestTimeFix_SelectI_DBLevel(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE harmony_task_test (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		update_time TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		posted_time TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// Insert two rows. posted_time uses an explicit literal so the
	// stored shape mirrors how modernc emits CURRENT_TIMESTAMP under
	// upstream curio's INSERT path.
	if _, err := db.ExecCount(ctx, `INSERT INTO harmony_task_test (name, posted_time) VALUES ('a', CURRENT_TIMESTAMP), ('b', '2026-05-23 18:25:48')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Exercise (*DB).SelectI \u2192 (*DB).Select (the hand-rolled reflector
	// path; this is what scheduler.go:438 hits in upstream).
	var rows []timeRow
	if err := db.SelectI(ctx, &rows, harmonyquery.RawString(`SELECT id, name, update_time, posted_time FROM harmony_task_test ORDER BY id`)); err != nil {
		t.Fatalf("SelectI: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("SelectI: got %d rows, want 2", len(rows))
	}
	if rows[0].Name != "a" || rows[1].Name != "b" {
		t.Errorf("SelectI: names = %q,%q; want a,b", rows[0].Name, rows[1].Name)
	}
	if rows[0].UpdateTime.IsZero() || rows[0].PostedTime.IsZero() {
		t.Errorf("SelectI: row 0 has zero times: update=%v posted=%v", rows[0].UpdateTime, rows[0].PostedTime)
	}
	if rows[1].PostedTime.Year() != 2026 || rows[1].PostedTime.Month() != time.May {
		t.Errorf("SelectI: row 1 posted_time = %v, want May 2026", rows[1].PostedTime)
	}
}

func TestTimeFix_SelectI_TxLevel(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE harmony_task_test (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		update_time TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		posted_time TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO harmony_task_test (name, posted_time) VALUES ('a', '2026-05-23 18:25:48.123456789'), ('b', '2026-05-24 09:10:11')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Exercise (*sqliteTxI).SelectI \u2192 dbscan.ScanAll(rowsWithTimeFix).
	// This is the path that fired the curio-core#17 error for the
	// task_type_handler.go:164 SelectI call site.
	var rows []timeRow
	committed, err := db.BeginTransactionI(ctx, func(tx harmonyquery.TxInterface) (bool, error) {
		return true, tx.SelectI(&rows, harmonyquery.RawString(`SELECT id, name, update_time, posted_time FROM harmony_task_test ORDER BY id`))
	})
	if err != nil {
		t.Fatalf("BeginTransactionI: %v", err)
	}
	if !committed {
		t.Fatalf("BeginTransactionI: not committed")
	}
	if len(rows) != 2 {
		t.Fatalf("SelectI: got %d rows, want 2", len(rows))
	}
	if rows[0].PostedTime.IsZero() || rows[1].PostedTime.IsZero() {
		t.Errorf("SelectI: zero times: %+v", rows)
	}
	if rows[0].PostedTime.Nanosecond() == 0 {
		t.Errorf("SelectI: row 0 lost sub-second precision: %v", rows[0].PostedTime)
	}
}

// TestTimeFix_QueryRowI confirms the previously-existing scanWithTimeFix
// wiring on QueryRowI continues to work after the new rowsWithTimeFix
// + (*DB).Select wiring landed in the same edit.
func TestTimeFix_QueryRowI(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE t (ts TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO t (ts) VALUES (CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	var got time.Time
	if err := db.QueryRowI(ctx, harmonyquery.RawString(`SELECT ts FROM t LIMIT 1`)).Scan(&got); err != nil {
		t.Fatalf("QueryRowI.Scan: %v", err)
	}
	if got.IsZero() {
		t.Errorf("QueryRowI: scanned zero time")
	}
}

// TaskIDScalar mirrors upstream harmonytask.TaskID — a typed int64.
// The scalar-slice path must work for typed scalars, not just bare ints.
type TaskIDScalar int64

func TestSelect_ScalarSlice_Int64(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO t (name) VALUES ('a'), ('b'), ('c')`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	var ids []int64
	if err := db.Select(ctx, &ids, `SELECT id FROM t ORDER BY id`); err != nil {
		t.Fatalf("Select int64: %v", err)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Errorf("Select int64: got %v, want [1 2 3]", ids)
	}
}

func TestSelect_ScalarSlice_TypedInt64(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY AUTOINCREMENT)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO t DEFAULT VALUES`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO t DEFAULT VALUES`); err != nil {
		t.Fatalf("INSERT 2: %v", err)
	}

	// This mirrors task_type_handler.go:181's *[]TaskID destination.
	var ids []TaskIDScalar
	if err := db.Select(ctx, &ids, `SELECT id FROM t ORDER BY id`); err != nil {
		t.Fatalf("Select TaskIDScalar: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Errorf("Select TaskIDScalar: got %v, want [1 2]", ids)
	}
}

func TestSelect_ScalarSlice_RejectsMultiColumn(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE t (a INTEGER, b INTEGER)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO t (a, b) VALUES (1, 2)`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	var ids []int64
	err = db.Select(ctx, &ids, `SELECT a, b FROM t`)
	if err == nil {
		t.Fatal("expected error on scalar-slice with 2 columns")
	}
	if !strings.Contains(err.Error(), "exactly 1 column") {
		t.Errorf("expected 'exactly 1 column' error, got: %v", err)
	}
}

// TestParseSQLiteTime_GoStringFormat covers the shape modernc.org/sqlite
// stores when a Go time.Time is passed as a driver.Value: Go's default
// time.Time.String() output, with optional monotonic-clock suffix.
//
// Real-world example caught against the harmonytask singleton path:
// last_run_time field stored as
// "2026-05-24 09:44:38.218610887 +0000 UTC m=+2.176855639"
// which time.Parse rejects.
func TestParseSQLiteTime_GoStringFormat(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool // true = should parse cleanly
	}{
		{"with monotonic", "2026-05-24 09:44:38.218610887 +0000 UTC m=+2.176855639", true},
		{"with negative monotonic", "2026-05-24 09:44:38.218610887 +0000 UTC m=-2.176855639", true},
		{"no monotonic", "2026-05-24 09:44:38.218610887 +0000 UTC", true},
		{"with positive offset", "2026-05-24 11:44:38.218610887 +0200 CEST", true},
		{"sqlite default", "2026-05-24 09:44:38", true},
		{"sqlite nanos", "2026-05-24 09:44:38.218610887", true},
		{"RFC3339Nano", "2026-05-24T09:44:38.218610887Z", true},
		{"garbage", "not a time", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, err := parseSQLiteTime(c.in)
			if c.want && err != nil {
				t.Errorf("parseSQLiteTime(%q) returned err: %v", c.in, err)
			}
			if !c.want && err == nil {
				t.Errorf("parseSQLiteTime(%q) expected err, got %v", c.in, ts)
			}
			if c.want && err == nil && ts.IsZero() {
				t.Errorf("parseSQLiteTime(%q) returned zero time", c.in)
			}
		})
	}
}
