package parkcomplete

import (
	"context"
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

// newTestStashFile creates an empty stash file at <dir>/<name>.tmp and
// returns the full path. Used by tests that need a real file present
// for the file-exists safety net to pass.
func newTestStashFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write stash file: %v", err)
	}
	return path
}

// seedPiece writes a parked_pieces row (complete=0, long_term=1) and a
// matching parked_piece_refs row with the given data_url. Returns the
// piece_id.
func seedPiece(t *testing.T, db *harmonysqlite.DB, pieceCid, dataURL string) int64 {
	t.Helper()
	ctx := context.Background()

	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, ?, ?, 0, 1)`,
		pieceCid, 8192, 4096); err != nil {
		t.Fatalf("insert parked_pieces: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	if err := row.Scan(&pieceID); err != nil {
		t.Fatalf("scan piece id: %v", err)
	}

	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, dataURL); err != nil {
		t.Fatalf("insert parked_piece_refs: %v", err)
	}
	return pieceID
}

// completeBit reads parked_pieces.complete for a piece.
func completeBit(t *testing.T, db *harmonysqlite.DB, pieceID int64) int {
	t.Helper()
	var c int
	row := db.QueryRow(context.Background(),
		`SELECT complete FROM parked_pieces WHERE id = ?`, pieceID)
	if err := row.Scan(&c); err != nil {
		t.Fatalf("scan complete: %v", err)
	}
	return c
}

// custoreURL builds a custore:// URL with the path the diskstash
// + handleStreamingUpload pair would actually emit: scheme=custore,
// path is the absolute filesystem path to the stash file.
func custoreURL(stashDir, name string) string {
	return "custore://" + filepath.Join(stashDir, name+".tmp")
}

// TestDo_FlipsCustoreCandidates: the canonical path. A streaming-upload
// row with a custore:// data_url and a real stash file gets flipped to
// complete=1 by one Do() invocation.
func TestDo_FlipsCustoreCandidates(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	stashName := "11111111-2222-3333-4444-555555555555"
	newTestStashFile(t, stashDir, stashName)
	pieceID := seedPiece(t, db,
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy",
		custoreURL(stashDir, stashName))

	if completeBit(t, db, pieceID) != 0 {
		t.Fatal("piece pre-state complete!=0")
	}

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := completeBit(t, db, pieceID); got != 1 {
		t.Errorf("complete after Do = %d, want 1", got)
	}
}

// TestDo_SkipsNonCustoreScheme: a piece with a non-custore data_url
// (e.g. a future http:// pull-piece source) is left alone. parkcomplete
// is curio-core-streaming-upload-specific; other code paths are
// responsible for their own complete-flip.
func TestDo_SkipsNonCustoreScheme(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	pieceID := seedPiece(t, db,
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy",
		"https://example.com/some-piece.bin")

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := completeBit(t, db, pieceID); got != 0 {
		t.Errorf("non-custore piece complete = %d, want 0 (untouched)", got)
	}
}

// TestDo_SkipsAbsentStashFile: a custore:// row whose stash file
// doesn't exist on disk gets skipped (no error, no complete-flip).
// Operator-wiped-stash-dir scenario.
func TestDo_SkipsAbsentStashFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	pieceID := seedPiece(t, db,
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy",
		custoreURL(stashDir, "orphaned-uuid"))
	// NOTE: no stash file created.

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if got := completeBit(t, db, pieceID); got != 0 {
		t.Errorf("orphaned piece complete = %d, want 0 (skipped)", got)
	}
}

// TestDo_IdempotentOnAlreadyComplete: re-running over an already-flipped
// piece is a no-op (no error, no double-flip).
func TestDo_IdempotentOnAlreadyComplete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	stashName := "abc"
	newTestStashFile(t, stashDir, stashName)
	pieceID := seedPiece(t, db,
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy",
		custoreURL(stashDir, stashName))

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do (1): %v", err)
	}
	if got := completeBit(t, db, pieceID); got != 1 {
		t.Fatalf("after Do(1): complete = %d", got)
	}

	// Second run should find no candidates and exit cleanly.
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Errorf("Do (2): %v", err)
	}
	if got := completeBit(t, db, pieceID); got != 1 {
		t.Errorf("after Do(2): complete = %d, want 1", got)
	}
}

// TestDo_MultiplePieces: batch advances multiple eligible pieces in
// one cycle.
func TestDo_MultiplePieces(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	stashes := []string{"piece-a", "piece-b", "piece-c"}
	pieceIDs := make([]int64, 0, len(stashes))
	for _, name := range stashes {
		newTestStashFile(t, stashDir, name)
		// Different CIDs per piece so the unique constraint on
		// parked_pieces.piece_cid (if any) doesn't fire. (We don't
		// actually have a unique constraint, but multi-piece tests
		// should still use distinct CIDs as a hygiene matter.)
		cid := "baga6ea4seaq" + name + "00000000000000000000000000000000000000000000000000000000"
		pieceIDs = append(pieceIDs, seedPiece(t, db, cid, custoreURL(stashDir, name)))
	}

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, func() bool { return true }); err != nil {
		t.Fatalf("Do: %v", err)
	}

	for _, id := range pieceIDs {
		if got := completeBit(t, db, id); got != 1 {
			t.Errorf("piece %d: complete = %d, want 1", id, got)
		}
	}
}

// TestDo_RespectStillOwned: when stillOwned() returns false mid-batch,
// Do() returns cleanly without flipping any remaining pieces.
func TestDo_RespectStillOwned(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	stashDir := t.TempDir()

	for _, name := range []string{"piece-x", "piece-y"} {
		newTestStashFile(t, stashDir, name)
		cid := "baga6ea4seaq" + name + "00000000000000000000000000000000000000000000000000000000"
		seedPiece(t, db, cid, custoreURL(stashDir, name))
	}

	calls := 0
	stillOwned := func() bool {
		calls++
		// Allow first iteration, then deny.
		return calls <= 1
	}

	task := New(db, stashDir)
	if _, err := task.Do(ctx, 1, stillOwned); err != nil {
		t.Fatalf("Do: %v", err)
	}

	// At most one piece should be complete (the first iteration).
	var completed int
	row := db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_pieces WHERE complete = 1`)
	if err := row.Scan(&completed); err != nil {
		t.Fatalf("count complete: %v", err)
	}
	if completed > 1 {
		t.Errorf("stillOwned ignored: %d pieces complete, want <= 1", completed)
	}
}

// TestStashPathFromCustoreURL covers the URL-to-path resolution.
// The handleStreamingUpload path emits URLs with the absolute
// filesystem path embedded; we extract it and sanity-check it sits
// inside the configured stash directory.
func TestStashPathFromCustoreURL(t *testing.T) {
	stashDir := t.TempDir()
	cases := []struct {
		name, in   string
		wantSuffix string
		err        bool
	}{
		{"inside-stash", "custore://" + filepath.Join(stashDir, "abc.tmp"), "/abc.tmp", false},
		{"outside-stash", "custore:///etc/passwd", "", true},
		{"wrong-scheme", "http://example.com/abc.tmp", "", true},
		{"empty-path", "custore://", "", true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got, err := stashPathFromCustoreURL(c.in, stashDir)
			if c.err {
				if err == nil {
					t.Errorf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !filepath.IsAbs(got) {
				t.Errorf("got non-absolute path: %q", got)
			}
			if filepath.Base(got) != filepath.Base(c.wantSuffix) {
				t.Errorf("got base %q, want %q", filepath.Base(got), filepath.Base(c.wantSuffix))
			}
		})
	}
}
