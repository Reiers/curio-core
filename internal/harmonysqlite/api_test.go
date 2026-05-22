package harmonysqlite

import (
	"context"
	"testing"
)

type taskRow struct {
	ID    int64  `db:"id"`
	State string `db:"state"`
	Owner string `db:"owner"`
}

// TestApi_FullRoundTrip exercises the upstream-shaped surface:
// CREATE → INSERT (via ExecCount) → BeginTransaction with claim →
// Select into a slice of structs.
func TestApi_FullRoundTrip(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		state TEXT NOT NULL,
		owner TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	n, err := db.ExecCount(ctx, `INSERT INTO tasks (state) VALUES (?), (?), (?)`, "queued", "queued", "queued")
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if n != 3 {
		t.Errorf("INSERT count = %d, want 3", n)
	}

	// Claim one via BeginTransaction.
	committed, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
		updated, err := tx.Exec(`UPDATE tasks SET state='running', owner=? WHERE id = (SELECT id FROM tasks WHERE state='queued' LIMIT 1)`, "worker-1")
		if err != nil {
			return false, err
		}
		if updated != 1 {
			t.Errorf("UPDATE in tx affected %d rows, want 1", updated)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if !committed {
		t.Error("expected commit, got rollback")
	}

	// Select all rows into a slice.
	var rows []taskRow
	if err := db.Select(ctx, &rows, `SELECT id, state, owner FROM tasks ORDER BY id`); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("Select returned %d rows, want 3", len(rows))
	}
	running := 0
	queued := 0
	for _, r := range rows {
		switch r.State {
		case "running":
			running++
			if r.Owner != "worker-1" {
				t.Errorf("running row has owner %q, want worker-1", r.Owner)
			}
		case "queued":
			queued++
		}
	}
	if running != 1 || queued != 2 {
		t.Errorf("counts: running=%d queued=%d, want 1/2", running, queued)
	}
}

// TestApi_TransactionRollback verifies rollback paths work.
func TestApi_TransactionRollback(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecCount(ctx, `CREATE TABLE counter (v INTEGER)`); err != nil {
		t.Fatalf("CREATE: %v", err)
	}
	if _, err := db.ExecCount(ctx, `INSERT INTO counter (v) VALUES (0)`); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Mutate inside tx but return commit=false.
	committed, err := db.BeginTransaction(ctx, func(tx *Tx) (bool, error) {
		if _, err := tx.Exec(`UPDATE counter SET v = 42`); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if committed {
		t.Error("expected rollback, got commit")
	}
	var v int
	if err := db.QueryRow(ctx, `SELECT v FROM counter`).Scan(&v); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if v != 0 {
		t.Errorf("counter = %d, want 0 (rollback should have reverted)", v)
	}
}
