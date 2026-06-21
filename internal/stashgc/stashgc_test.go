package stashgc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func newTestDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{
		Path:        ":memory:",
		ForeignKeys: true,
	})
	if err != nil {
		t.Fatalf("harmonysqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// writeStash writes a stash file and back-dates its mtime by age.
func writeStash(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatalf("chtimes %s: %v", p, err)
	}
	return p
}

// ref inserts a parked_piece (complete flag configurable) + a custore ref
// pointing at path.
func ref(t *testing.T, db *harmonysqlite.DB, path string, complete int) {
	t.Helper()
	ctx := context.Background()
	cid := fmt.Sprintf("baga-%s", filepath.Base(path))
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		 VALUES (?, 256, 200, ?, 1)`, cid, complete); err != nil {
		t.Fatalf("insert piece: %v", err)
	}
	var id int64
	if err := db.QueryRowI(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, cid).Scan(&id); err != nil {
		t.Fatalf("select id: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		id, "custore://"+path); err != nil {
		t.Fatalf("insert ref: %v", err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Acceptance: old orphans deleted; referenced + recent files kept;
// complete-backed files never touched.
func TestSweep_Armed(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stash := t.TempDir()

	orphanOld := writeStash(t, stash, "orphan-old", 48*time.Hour) // delete
	orphanNew := writeStash(t, stash, "orphan-new", 1*time.Minute) // keep (too new)
	referenced := writeStash(t, stash, "referenced", 48*time.Hour) // keep (ref'd, incomplete)
	completeBacked := writeStash(t, stash, "complete", 48*time.Hour) // keep (complete-backed)

	ref(t, db, referenced, 0)     // incomplete ref -> live
	ref(t, db, completeBacked, 1) // complete ref -> never touch

	task := New(db, Config{StashDir: stash, Retention: 24 * time.Hour, DryRun: false})
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if exists(orphanOld) {
		t.Errorf("old orphan should be deleted")
	}
	if !exists(orphanNew) {
		t.Errorf("recent orphan should be kept (retention floor)")
	}
	if !exists(referenced) {
		t.Errorf("referenced file should be kept")
	}
	if !exists(completeBacked) {
		t.Errorf("complete-backed file must NEVER be deleted")
	}
}

// Dry-run deletes nothing.
func TestSweep_DryRun(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stash := t.TempDir()

	orphan := writeStash(t, stash, "orphan", 48*time.Hour)
	task := New(db, Config{StashDir: stash, Retention: 24 * time.Hour, DryRun: true})
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !exists(orphan) {
		t.Errorf("dry-run must not delete anything")
	}
}

// Default config is safe: dry-run defaults true only when explicitly set;
// here we assert the retention/poll defaults resolve.
func TestConfigDefaults(t *testing.T) {
	c := Config{}
	if c.retention() != DefaultRetention {
		t.Errorf("retention default = %v, want %v", c.retention(), DefaultRetention)
	}
	if c.pollInterval() != DefaultPollInterval {
		t.Errorf("poll default = %v, want %v", c.pollInterval(), DefaultPollInterval)
	}
}
