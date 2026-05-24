// Package localpiecepark serves piece bytes from curio-core's diskstash
// to the cachedreader piece-park path. It's the curio-core-shape
// substitute for upstream Curio's pieceprovider.PieceParkReader,
// which routes through the cluster-aware paths.Remote +
// paths.SectorIndex storage abstraction.
//
// The architectural shift: in upstream Curio, the streaming-upload
// path writes bytes to a stash file and ParkPieceTask later copies
// them into long-term cluster storage (where PieceParkReader resolves
// them via PathByType + AcquireSector + the worker-network ladder).
// In curio-core, stash IS the long-term storage \u2014 the bytes never
// move. So our PieceParkBackend implementation skips the entire
// cluster lookup and reads directly from the local file.
//
// Why a separate package: keeps the curio-core SQLite + filesystem
// surface isolated from the upstream pieceprovider concrete type.
// The interface (Reiers/curio@2754b58 pieceprovider.PieceParkBackend)
// is the contract; this implementation satisfies it via the
// parked_pieces + parked_piece_refs tables we already populate.

package localpiecepark

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// Reader serves piece bytes by looking up the parked_pieces.id ->
// parked_piece_refs.data_url -> filesystem path chain, then opening
// the file. Construct via New.
//
// Implements pieceprovider.PieceParkBackend.
type Reader struct {
	db       *harmonysqlite.DB
	stashDir string
}

// New returns a Reader backed by the given DB + stash directory.
// stashDir is the directory diskstash writes streaming-upload files
// into; used as a safety boundary for resolved paths (any path that
// resolves outside stashDir is rejected).
func New(db *harmonysqlite.DB, stashDir string) *Reader {
	return &Reader{db: db, stashDir: stashDir}
}

// Compile-time guard against interface drift.
var _ pieceprovider.PieceParkBackend = (*Reader)(nil)

// ReadPiece resolves a parked_pieces row to its on-disk stash file
// and returns an *os.File-backed storiface.Reader over it.
//
// Looks up via parked_pieces.id (= storiface.PieceNumber). The
// pieceSize + pc arguments are validated against the DB row to
// catch caller / DB drift:
//
//   - pieceSize must match parked_pieces.piece_raw_size
//   - pc (CIDv1) must match parked_pieces.piece_cid
//
// On mismatch, returns an error without opening the file.
func (r *Reader) ReadPiece(ctx context.Context, pieceParkID storiface.PieceNumber, pieceSize int64, pc cid.Cid) (storiface.Reader, error) {
	id := int64(pieceParkID)

	// Look up the parked piece + its primary data URL. parked_piece_refs
	// can have multiple rows per piece (one per reference), but all
	// point to the same underlying bytes \u2014 we take the first row
	// (lowest ref_id) for determinism.
	var (
		pieceCidStr string
		rawSize     int64
		complete    int
		dataURL     string
	)
	row := r.db.QueryRow(ctx, `
		SELECT pp.piece_cid, pp.piece_raw_size, pp.complete,
		       (SELECT pr.data_url FROM parked_piece_refs pr
		         WHERE pr.piece_id = pp.id
		         ORDER BY pr.ref_id ASC LIMIT 1) AS data_url
		FROM parked_pieces pp
		WHERE pp.id = ?`, id)
	if err := row.Scan(&pieceCidStr, &rawSize, &complete, &dataURL); err != nil {
		return nil, fmt.Errorf("localpiecepark: lookup parked_pieces.id=%d: %w", id, err)
	}
	if complete != 1 {
		return nil, fmt.Errorf("localpiecepark: piece %d not complete (parked_pieces.complete=%d)", id, complete)
	}
	if dataURL == "" {
		return nil, fmt.Errorf("localpiecepark: piece %d has no parked_piece_refs row", id)
	}

	// Validate the request matches the DB row.
	if pieceSize != 0 && pieceSize != rawSize {
		return nil, fmt.Errorf("localpiecepark: piece %d size mismatch: requested %d, DB %d",
			id, pieceSize, rawSize)
	}
	if pc.Defined() {
		dbCid, err := cid.Decode(pieceCidStr)
		if err != nil {
			return nil, fmt.Errorf("localpiecepark: decode DB piece_cid %q: %w", pieceCidStr, err)
		}
		if !dbCid.Equals(pc) {
			return nil, fmt.Errorf("localpiecepark: piece %d cid mismatch: requested %s, DB %s",
				id, pc.String(), dbCid.String())
		}
	}

	// Resolve the custore:// URL to a filesystem path. Same safety
	// boundary as parkcomplete: must sit inside stashDir.
	path, err := stashPathFromCustoreURL(dataURL, r.stashDir)
	if err != nil {
		return nil, fmt.Errorf("localpiecepark: resolve data_url for piece %d: %w", id, err)
	}

	// storiface.Reader = io.Closer + io.Reader + io.ReaderAt + io.Seeker.
	// *os.File satisfies all four.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("localpiecepark: open %s for piece %d: %w", path, id, err)
	}
	return f, nil
}

// stashPathFromCustoreURL converts the custore:// data URL into the
// on-disk path. Mirrors the parkcomplete helper of the same name;
// each has its own copy so the packages stay independent and the
// boundary check is duplicated (defense in depth against either
// being bypassed).
//
// The URL shape is custore://<absolute-fs-path>, written by upstream
// pdp/handlers_upload.go from diskstash.StashURL output with the
// scheme rewritten.
func stashPathFromCustoreURL(dataURL, stashDir string) (string, error) {
	u, err := url.Parse(dataURL)
	if err != nil {
		return "", fmt.Errorf("parse custore URL: %w", err)
	}
	if u.Scheme != "custore" {
		return "", fmt.Errorf("expected custore scheme, got %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("custore URL %q has no path component", dataURL)
	}
	absStashDir, err := filepath.Abs(stashDir)
	if err != nil {
		return "", fmt.Errorf("resolve stash dir abs path: %w", err)
	}
	candidate := filepath.Clean(u.Path)
	rel, err := filepath.Rel(absStashDir, candidate)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("custore URL path %q is not inside stash dir %q", candidate, absStashDir)
	}
	return candidate, nil
}
