package localpiecepark

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/curio/lib/storiface"

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

// custoreURL builds a custore:// URL pointing inside stashDir, matching
// what diskstash + handleStreamingUpload emit in production.
func custoreURL(stashDir, name string) string {
	return "custore://" + filepath.Join(stashDir, name+".tmp")
}

// seedComplete inserts a parked_pieces row (complete=1) + matching
// parked_piece_refs row and writes a stash file with content. Returns
// the piece_id.
func seedComplete(t *testing.T, db *harmonysqlite.DB, stashDir, name, pieceCid string, content []byte) int64 {
	t.Helper()
	ctx := context.Background()

	// Write the stash file.
	path := filepath.Join(stashDir, name+".tmp")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write stash file: %v", err)
	}

	// parked_pieces row.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, ?, ?, 1, 1)`,
		pieceCid, len(content)*2, len(content)); err != nil {
		t.Fatalf("insert parked_pieces: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	if err := row.Scan(&pieceID); err != nil {
		t.Fatalf("scan piece id: %v", err)
	}

	// parked_piece_refs row.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, custoreURL(stashDir, name)); err != nil {
		t.Fatalf("insert parked_piece_refs: %v", err)
	}
	return pieceID
}

// TestReadPiece_HappyPath: a complete piece round-trips through the
// reader and the bytes match what was written.
func TestReadPiece_HappyPath(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	want := []byte("hello pdp world; this is a test piece body")

	pieceID := seedComplete(t, db, stashDir, "hp",
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy", want)

	r := New(db, stashDir)
	reader, err := r.ReadPiece(context.Background(), storiface.PieceNumber(pieceID), int64(len(want)), cid.Undef)
	if err != nil {
		t.Fatalf("ReadPiece: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("bytes mismatch: got %d, want %d", len(got), len(want))
	}
}

// TestReadPiece_ReaderAtAndSeeker: the returned reader satisfies the
// full storiface.Reader contract (io.ReaderAt + io.Seeker). Required
// because the proof code reads non-sequentially.
func TestReadPiece_ReaderAtAndSeeker(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	want := []byte("0123456789ABCDEF")

	pieceID := seedComplete(t, db, stashDir, "ra",
		"baga6ea4seaqh5ikj3g4e3ipmun3b2icgv3eenetxp4vqoanjkxtnmggcfgap4bq", want)

	r := New(db, stashDir)
	reader, err := r.ReadPiece(context.Background(), storiface.PieceNumber(pieceID), int64(len(want)), cid.Undef)
	if err != nil {
		t.Fatalf("ReadPiece: %v", err)
	}
	defer reader.Close()

	// ReaderAt: jump to offset 5, read 6 bytes.
	got := make([]byte, 6)
	if _, err := reader.ReadAt(got, 5); err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if string(got) != "56789A" {
		t.Errorf("ReadAt = %q, want %q", got, "56789A")
	}

	// Seek + Read: jump to offset 10, read to end.
	if _, err := reader.Seek(10, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	tail, _ := io.ReadAll(reader)
	if string(tail) != "ABCDEF" {
		t.Errorf("Seek+Read tail = %q, want %q", tail, "ABCDEF")
	}
}

// TestReadPiece_NotComplete: a piece with complete=0 (no parkcomplete
// pass yet) is refused.
func TestReadPiece_NotComplete(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	ctx := context.Background()

	pieceCid := "baga6ea4seaql5aoceurz3hmiqyruprhqkjgyw43akjf7nckbykuoumskwuazudy"
	// Insert with complete=0.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, 8192, 4096, 0, 1)`, pieceCid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	_ = row.Scan(&pieceID)

	// Even with a refs row, complete=0 should fail.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, custoreURL(stashDir, "nc")); err != nil {
		t.Fatalf("insert refs: %v", err)
	}

	r := New(db, stashDir)
	_, err := r.ReadPiece(ctx, storiface.PieceNumber(pieceID), 0, cid.Undef)
	if err == nil {
		t.Fatal("expected error on incomplete piece, got nil")
	}
}

// TestReadPiece_NotFound: a parked_pieces.id that doesn't exist
// produces an error, not a nil reader.
func TestReadPiece_NotFound(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()

	r := New(db, stashDir)
	_, err := r.ReadPiece(context.Background(), storiface.PieceNumber(99999), 0, cid.Undef)
	if err == nil {
		t.Fatal("expected error on unknown piece, got nil")
	}
}

// TestReadPiece_SizeMismatch: requesting a size that doesn't match the
// stored row is a caller bug; surface as error.
func TestReadPiece_SizeMismatch(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	pieceID := seedComplete(t, db, stashDir, "sm",
		"baga6ea4seaqoitdtmeewuapeqf7lwjkmgdo6q2whutqy24wlxdffjdbj32nt2my",
		bytes.Repeat([]byte("x"), 100))

	r := New(db, stashDir)
	_, err := r.ReadPiece(context.Background(), storiface.PieceNumber(pieceID), 50 /* wrong */, cid.Undef)
	if err == nil {
		t.Fatal("expected size-mismatch error, got nil")
	}
}

// TestReadPiece_CidMismatch: same idea, on CID.
func TestReadPiece_CidMismatch(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	pieceID := seedComplete(t, db, stashDir, "cm",
		"baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy",
		[]byte("body"))

	wrongCid, _ := cid.Decode("baga6ea4seaqh5ikj3g4e3ipmun3b2icgv3eenetxp4vqoanjkxtnmggcfgap4bq")
	r := New(db, stashDir)
	_, err := r.ReadPiece(context.Background(), storiface.PieceNumber(pieceID), 0, wrongCid)
	if err == nil {
		t.Fatal("expected cid-mismatch error, got nil")
	}
}

// TestReadPiece_OutsideStashDir: a malformed data_url pointing outside
// the configured stashDir is rejected (path traversal defense).
func TestReadPiece_OutsideStashDir(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	ctx := context.Background()

	pieceCid := "baga6ea4seaqhisdvtbfvi4j7ovfdqphygr7w6jpqvbzy3lnkpv4fftcnciqaakq"
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, 8192, 4096, 1, 1)`, pieceCid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	_ = row.Scan(&pieceID)

	// data_url points outside stashDir.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, "custore:///etc/passwd"); err != nil {
		t.Fatalf("insert refs: %v", err)
	}

	r := New(db, stashDir)
	_, err := r.ReadPiece(ctx, storiface.PieceNumber(pieceID), 0, cid.Undef)
	if err == nil {
		t.Fatal("expected outside-stash error, got nil")
	}
}

// TestReadPiece_AbsentFile: DB row points at a file that's been deleted
// off disk (operator wiped stash, etc).
func TestReadPiece_AbsentFile(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	ctx := context.Background()

	// Seed without writing the file.
	pieceCid := "baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy"
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, 8192, 4096, 1, 1)`, pieceCid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	_ = row.Scan(&pieceID)

	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id, data_url, long_term) VALUES (?, ?, 1)`,
		pieceID, custoreURL(stashDir, "gone")); err != nil {
		t.Fatalf("insert refs: %v", err)
	}

	r := New(db, stashDir)
	_, err := r.ReadPiece(ctx, storiface.PieceNumber(pieceID), 0, cid.Undef)
	if err == nil {
		t.Fatal("expected open-file error, got nil")
	}
}

// TestReadPiece_NoRefs: a parked_pieces row with no parked_piece_refs
// entries is unusable.
func TestReadPiece_NoRefs(t *testing.T) {
	db := newTestDB(t)
	stashDir := t.TempDir()
	ctx := context.Background()

	pieceCid := "baga6ea4seaqh5ikj3g4e3ipmun3b2icgv3eenetxp4vqoanjkxtnmggcfgap4bq"
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES (?, 8192, 4096, 1, 1)`, pieceCid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var pieceID int64
	row := db.QueryRow(ctx, `SELECT id FROM parked_pieces WHERE piece_cid = ?`, pieceCid)
	_ = row.Scan(&pieceID)

	r := New(db, stashDir)
	_, err := r.ReadPiece(ctx, storiface.PieceNumber(pieceID), 0, cid.Undef)
	if err == nil {
		t.Fatal("expected no-refs error, got nil")
	}
}
