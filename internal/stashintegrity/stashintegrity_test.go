package stashintegrity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

// seedComplete inserts a complete=1 parked_pieces row + a custore ref whose
// data_url points at <stashDir>/<name>.tmp. Returns the piece id and path.
func seedComplete(t *testing.T, db *harmonysqlite.DB, stashDir, pieceCid, name string) (int64, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		 VALUES (?, 256, 200, 1, 1)`, pieceCid); err != nil {
		t.Fatalf("insert parked_pieces: %v", err)
	}
	var pieceID int64
	if err := db.QueryRowI(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid).Scan(&pieceID); err != nil {
		t.Fatalf("select piece id: %v", err)
	}
	path := filepath.Join(stashDir, name+".tmp")
	dataURL := fmt.Sprintf("custore://%s", path)
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, dataURL); err != nil {
		t.Fatalf("insert parked_piece_refs: %v", err)
	}
	return pieceID, path
}

func missingAt(t *testing.T, db *harmonysqlite.DB, pieceID int64) (string, bool) {
	t.Helper()
	var v *string
	if err := db.QueryRowI(context.Background(),
		`SELECT integrity_missing_at FROM parked_pieces WHERE id = ?`, pieceID).Scan(&v); err != nil {
		t.Fatalf("select integrity_missing_at: %v", err)
	}
	if v == nil {
		return "", false
	}
	return *v, true
}

// Acceptance: a complete piece whose stash file is gone gets flagged; a
// complete piece whose file is present is left alone.
func TestSweep_FlagsMissingNotPresent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stash := t.TempDir()

	// present: write the file
	presentID, presentPath := seedComplete(t, db, stash, "baga-present", "present")
	if err := os.WriteFile(presentPath, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("write present file: %v", err)
	}
	// missing: do NOT create the file
	missingID, _ := seedComplete(t, db, stash, "baga-missing", "missing")

	task := New(db, stash)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if _, flagged := missingAt(t, db, presentID); flagged {
		t.Errorf("present piece should NOT be flagged")
	}
	if _, flagged := missingAt(t, db, missingID); !flagged {
		t.Errorf("missing piece SHOULD be flagged")
	}

	// An alert should have been emitted for the missing piece.
	var alertCount int64
	if err := db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM curio_alerts WHERE source = 'stashintegrity/missing-file'`).Scan(&alertCount); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if alertCount != 1 {
		t.Errorf("expected 1 alert, got %d", alertCount)
	}
}

// Acceptance: a file reappearing clears the flag.
func TestSweep_ClearsOnRecovery(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stash := t.TempDir()

	id, path := seedComplete(t, db, stash, "baga-recover", "recover")
	task := New(db, stash)

	// First pass: file missing -> flagged.
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do pass1: %v", err)
	}
	if _, flagged := missingAt(t, db, id); !flagged {
		t.Fatalf("piece should be flagged after pass1")
	}

	// File reappears.
	if err := os.WriteFile(path, []byte("recovered"), 0o600); err != nil {
		t.Fatalf("write recovered file: %v", err)
	}

	// Second pass: flag cleared.
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do pass2: %v", err)
	}
	if _, flagged := missingAt(t, db, id); flagged {
		t.Errorf("piece flag should be cleared after file recovered")
	}
}

// CountMissing reflects the flagged count.
func TestCountMissing(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stash := t.TempDir()

	seedComplete(t, db, stash, "baga-m1", "m1")
	seedComplete(t, db, stash, "baga-m2", "m2")
	okID, okPath := seedComplete(t, db, stash, "baga-ok", "ok")
	if err := os.WriteFile(okPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write ok file: %v", err)
	}
	_ = okID

	task := New(db, stash)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}
	n, err := CountMissing(ctx, db)
	if err != nil {
		t.Fatalf("CountMissing: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 missing, got %d", n)
	}
}
