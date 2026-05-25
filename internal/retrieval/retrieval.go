// Package retrieval serves piece bytes over HTTP from curio-core's
// parked_pieces + parked_piece_refs storage. Implements the read
// path that pairs with the existing /pdp/piece upload + /pdp/data-sets
// write path; without it, curio-core is a write-only black hole.
//
// Endpoint:
//
//	GET /piece/{pieceCid}    - stream the raw piece bytes
//
// Behavior:
//
//   - HTTP Range requests via http.ServeContent (handles
//     If-None-Match, If-Modified-Since, partial content, etc.)
//   - ETag = "<pieceCid>"  (pieces are content-addressed, immutable)
//   - Cache-Control: public, max-age=29030400, immutable (1 year)
//   - Content-Type: application/octet-stream + Accept-Ranges: bytes
//   - 404 for unknown / not-yet-parked pieces
//   - 503 if the database is unavailable
//
// Not implemented (intentionally, vs upstream's market/retrieval):
//
//   - No cachedreader / shared-reader pooling. curio-core serves
//     directly from the os.File handle returned by localpiecepark,
//     one open file per request. For a single-node SP this is fine;
//     the OS page cache handles re-reads.
//   - No denylist server polling. curio-core is opinionated:
//     anything stored is retrievable.
//   - No metrics / stats.Record. Add when we actually instrument
//     curio-core (#39 SP dashboard scope).
//   - No CommP verification on serve. The bytes were CommP-verified
//     at park time (pdp/handlers.go); we trust the on-disk state.
//
// Tracks: curio-core#36 (P1 hot-storage feature).
package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/localpiecepark"
)

// lastModified is a constant non-zero time used as the
// http.ServeContent modtime. Pieces are content-addressed and
// never change; this gives clients a stable If-Modified-Since
// target without exposing real filesystem mtimes.
var lastModified = time.UnixMilli(1)

// Backend is the read interface satisfied by localpiecepark.Reader.
// Kept narrow so tests can stub it without bringing in SQLite.
type Backend interface {
	ReadPiece(ctx context.Context, pieceParkID storiface.PieceNumber, pieceSize int64, pc cid.Cid) (storiface.Reader, error)
}

// Server holds the backend + DB needed to resolve a piece CID to
// its parked_pieces row and serve the underlying bytes.
type Server struct {
	db      *harmonysqlite.DB
	backend Backend
}

// New constructs a Server. dbPath / stashDir are already wired into
// the localpiecepark.Reader.
func New(db *harmonysqlite.DB, backend Backend) *Server {
	return &Server{db: db, backend: backend}
}

// Routes mounts:
//
//	GET /piece/{pieceCid}
//
// on the given router. Helper for the cmd-run wiring.
func Routes(r chi.Router, db *harmonysqlite.DB, stashDir string) *Server {
	backend := localpiecepark.New(db, stashDir)
	s := New(db, backend)
	r.Get("/piece/{pieceCid}", s.handleGetPiece)
	// http.ServeContent handles HEAD natively (skips body write), so
	// route the same handler. Without this chi returns 405.
	r.Head("/piece/{pieceCid}", s.handleGetPiece)
	return s
}

// handleGetPiece looks up the piece by CIDv1, opens the on-disk
// file via the backend, and streams it through http.ServeContent
// (which handles Range / If-Modified-Since / partial-content for
// us).
func (s *Server) handleGetPiece(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pieceCidStr := strings.TrimSpace(chi.URLParam(r, "pieceCid"))
	if pieceCidStr == "" {
		http.Error(w, "missing pieceCid path parameter", http.StatusBadRequest)
		return
	}

	pieceCid, err := cid.Decode(pieceCidStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid piece CID %q: %s", pieceCidStr, err), http.StatusBadRequest)
		return
	}

	// Look up the parked_pieces row by piece_cid. parked_pieces stores
	// CIDv1 in the piece_cid column (see schema 0008_piece_park.sql).
	// long_term filtering is intentionally absent: a Hot Storage SP
	// retrieves any complete piece it knows about regardless of the
	// long_term flag.
	var (
		pieceID int64
		rawSize int64
	)
	row := s.db.QueryRow(ctx, `
		SELECT id, piece_raw_size
		FROM parked_pieces
		WHERE piece_cid = ? AND complete = 1
		ORDER BY id ASC
		LIMIT 1`,
		pieceCid.String(),
	)
	if err := row.Scan(&pieceID, &rawSize); err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			http.Error(w, fmt.Sprintf("piece %s not found", pieceCid.String()), http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("lookup piece %s: %s", pieceCid.String(), err), http.StatusInternalServerError)
		return
	}

	// Open the piece bytes. localpiecepark returns a storiface.Reader
	// (io.Reader + io.Seeker + io.Closer + io.ReaderAt), backed by an
	// *os.File. http.ServeContent needs io.ReadSeeker, which we have.
	reader, err := s.backend.ReadPiece(ctx, storiface.PieceNumber(pieceID), rawSize, pieceCid)
	if err != nil {
		http.Error(w, fmt.Sprintf("open piece %s: %s", pieceCid.String(), err), http.StatusInternalServerError)
		return
	}
	defer func() { _ = reader.Close() }()

	// Constrain the served range to exactly the raw piece size, not
	// the padded on-disk size. Padding zeros are an implementation
	// detail of the storage layer and not part of the client's data.
	sr := io.NewSectionReader(reader, 0, rawSize)

	// Headers: content-addressed cache + ETag, Accept-Ranges via
	// ServeContent's built-in handling.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, pieceCid.String()))
	w.Header().Set("Cache-Control", "public, max-age=29030400, immutable")
	w.Header().Set("Vary", "Accept-Encoding")

	// http.ServeContent does Range / If-Modified-Since / partial
	// content / proper Content-Length for us. modtime is a constant
	// so clients get stable conditional-request behavior; the file
	// content for a given CID never changes.
	http.ServeContent(w, r, pieceCid.String(), lastModified, sr)
}
