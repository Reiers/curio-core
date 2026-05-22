package harmonysqlite

import (
	"context"
	"testing"
)

func TestOpen_RequiresPath(t *testing.T) {
	_, err := Open(Config{})
	if err == nil {
		t.Fatal("expected error for missing Path")
	}
}

func TestOpen_InMemory(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if got := db.Path(); got != ":memory:" {
		t.Errorf("Path() = %q, want :memory:", got)
	}
}

func TestOpen_FileBacked(t *testing.T) {
	tmp := t.TempDir() + "/test.sqlite"
	db, err := Open(Config{Path: tmp, WALMode: true, ForeignKeys: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Confirm we can do the basic harmonytask-shaped operation:
	// CREATE TABLE, INSERT, UPDATE inside a transaction, COMMIT,
	// SELECT result.
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE TABLE tasks (
		id INTEGER PRIMARY KEY,
		state TEXT NOT NULL,
		owner TEXT
	)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO tasks (state) VALUES (?), (?), (?)`,
		"queued", "queued", "queued"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Simulated "claim a task" pattern: BEGIN IMMEDIATE, UPDATE one row
	// to claim it, COMMIT.
	tx, err := db.BeginImmediate(ctx)
	if err != nil {
		t.Fatalf("BeginImmediate: %v", err)
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE tasks SET state = 'running', owner = ? WHERE id = (SELECT id FROM tasks WHERE state = 'queued' LIMIT 1)`,
		"worker-1")
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("UPDATE: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		t.Errorf("RowsAffected = %d, want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify post-commit state.
	var queued, running int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE state = 'queued'`).Scan(&queued); err != nil {
		t.Fatalf("SELECT queued: %v", err)
	}
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE state = 'running'`).Scan(&running); err != nil {
		t.Fatalf("SELECT running: %v", err)
	}
	if queued != 2 || running != 1 {
		t.Errorf("counts: queued=%d running=%d, want 2/1", queued, running)
	}
}

// TestPureGo confirms the harmonysqlite package brings in zero CGo deps.
// This is enforced indirectly: the package would fail to build with
// CGO_ENABLED=0 if any transitive dependency required CGo. We rely on
// the build-time check in CI; this test is a placeholder/anchor.
func TestPureGo(t *testing.T) {
	// No-op. The CGo-free property is enforced by CGO_ENABLED=0
	// builds elsewhere (CI workflow + the curio-core binary build).
	// This test exists to anchor the requirement in code.
}
