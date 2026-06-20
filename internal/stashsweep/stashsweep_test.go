package stashsweep

import (
	"context"
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

func custoreURL(path string) string { return "custore://" + path }

// seedComplete inserts a complete=1 parked_pieces row + a custore ref at
// stashDir/<name>.tmp. Returns (pieceID, fullStashPath). If createFile,
// the backing file is written.
func seedComplete(t *testing.T, db *harmonysqlite.DB, stashDir, name string, createFile bool) (int64, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(stashDir, name+".tmp")
	if createFile {
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatalf("write stash file: %v", err)
		}
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		 VALUES (?, ?, ?, 1, 1)`, name, 8192, 4096); err != nil {
		t.Fatalf("insert parked_pieces: %v", err)
	}
	var id int64
	if err := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, name).Scan(&id); err != nil {
		t.Fatalf("scan id: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		id, custoreURL(path)); err != nil {
		t.Fatalf("insert ref: %v", err)
	}
	return id, path
}

func missingAt(t *testing.T, db *harmonysqlite.DB, id int64) *string {
	t.Helper()
	var v *string
	if err := db.QueryRow(context.Background(),
		`SELECT integrity_missing_at FROM parked_pieces WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("scan integrity_missing_at: %v", err)
	}
	return v
}

func alwaysOwned() bool { return true }

func TestIntegrity_FlagsMissingFile(t *testing.T) {
	db := newTestDB(t)
	stash := t.TempDir()
	present, _ := seedComplete(t, db, stash, "present", true)
	gone, gonePath := seedComplete(t, db, stash, "gone", false)
	_ = gonePath

	task := New(db, Config{StashDir: stash})
	res, err := task.sweep(context.Background(), alwaysOwned)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.IntegrityBroken != 1 {
		t.Fatalf("IntegrityBroken=%d want 1 (%+v)", res.IntegrityBroken, res)
	}
	if missingAt(t, db, gone) == nil {
		t.Error("missing-file piece should be stamped integrity_missing_at")
	}
	if missingAt(t, db, present) != nil {
		t.Error("present-file piece must NOT be stamped")
	}
}

func TestIntegrity_HealsWhenFileReappears(t *testing.T) {
	db := newTestDB(t)
	stash := t.TempDir()
	id, path := seedComplete(t, db, stash, "flaky", false)

	task := New(db, Config{StashDir: stash})
	// first pass: broken
	if _, err := task.sweep(context.Background(), alwaysOwned); err != nil {
		t.Fatal(err)
	}
	if missingAt(t, db, id) == nil {
		t.Fatal("expected stamped after first pass")
	}
	// file reappears
	if err := os.WriteFile(path, []byte("back"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := task.sweep(context.Background(), alwaysOwned)
	if err != nil {
		t.Fatal(err)
	}
	if res.IntegrityHealed != 1 {
		t.Fatalf("IntegrityHealed=%d want 1", res.IntegrityHealed)
	}
	if missingAt(t, db, id) != nil {
		t.Error("flag should be cleared after heal")
	}
}

func TestGC_ReclaimsOrphansRespectingRetentionAndRefs(t *testing.T) {
	db := newTestDB(t)
	stash := t.TempDir()

	// referenced + present (complete piece): must NEVER be touched
	_, refPath := seedComplete(t, db, stash, "referenced", true)

	// orphan, old: should be reclaimed
	orphanOld := filepath.Join(stash, "orphan-old.tmp")
	if err := os.WriteFile(orphanOld, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphanOld, old, old); err != nil {
		t.Fatal(err)
	}

	// orphan, young: within retention, must be kept
	orphanYoung := filepath.Join(stash, "orphan-young.tmp")
	if err := os.WriteFile(orphanYoung, []byte("xx"), 0o600); err != nil {
		t.Fatal(err)
	}

	task := New(db, Config{StashDir: stash, GCEnabled: true, GCRetention: 24 * time.Hour})
	res, err := task.sweep(context.Background(), alwaysOwned)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.OrphanReclaimed != 1 || res.BytesReclaimed != 10 {
		t.Fatalf("reclaimed=%d bytes=%d want 1/10 (%+v)", res.OrphanReclaimed, res.BytesReclaimed, res)
	}
	if _, err := os.Stat(orphanOld); !os.IsNotExist(err) {
		t.Error("old orphan should be deleted")
	}
	if _, err := os.Stat(orphanYoung); err != nil {
		t.Error("young orphan must be kept (within retention)")
	}
	if _, err := os.Stat(refPath); err != nil {
		t.Error("referenced file must NEVER be deleted by GC")
	}
}

func TestGC_DryRunDeletesNothing(t *testing.T) {
	db := newTestDB(t)
	stash := t.TempDir()
	orphan := filepath.Join(stash, "orphan.tmp")
	if err := os.WriteFile(orphan, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(orphan, old, old)

	task := New(db, Config{StashDir: stash, GCEnabled: false, GCRetention: 24 * time.Hour})
	res, err := task.sweep(context.Background(), alwaysOwned)
	if err != nil {
		t.Fatal(err)
	}
	if res.OrphanCandidates != 1 {
		t.Fatalf("OrphanCandidates=%d want 1", res.OrphanCandidates)
	}
	if res.OrphanReclaimed != 0 {
		t.Fatalf("dry-run must not reclaim, got %d", res.OrphanReclaimed)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Error("dry-run must not delete the file")
	}
}

func TestGC_NeverDeletesFileBackingCompleteRow(t *testing.T) {
	// A complete=1 row whose file exists is referenced => off-limits to GC
	// even when old. (If the file were MISSING that's the integrity case.)
	db := newTestDB(t)
	stash := t.TempDir()
	_, path := seedComplete(t, db, stash, "live", true)
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	task := New(db, Config{StashDir: stash, GCEnabled: true, GCRetention: 24 * time.Hour})
	res, err := task.sweep(context.Background(), alwaysOwned)
	if err != nil {
		t.Fatal(err)
	}
	if res.OrphanReclaimed != 0 {
		t.Fatalf("must not reclaim a referenced file, got %d", res.OrphanReclaimed)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("referenced complete-row file must survive GC")
	}
}

func TestAlertFn_FiresOnNewlyBroken(t *testing.T) {
	db := newTestDB(t)
	stash := t.TempDir()
	seedComplete(t, db, stash, "gone", false)

	var alerts int
	task := New(db, Config{
		StashDir: stash,
		AlertFn: func(_ context.Context, source, _ string, _ map[string]any) {
			if source == "stashsweep/integrity" {
				alerts++
			}
		},
	})
	if _, err := task.sweep(context.Background(), alwaysOwned); err != nil {
		t.Fatal(err)
	}
	if alerts != 1 {
		t.Fatalf("expected 1 integrity alert, got %d", alerts)
	}
}
