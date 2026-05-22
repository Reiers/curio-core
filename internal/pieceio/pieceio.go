// Package pieceio defines the file-IO interface Curio Core uses in
// place of github.com/filecoin-project/curio/lib/ffi.
//
// Curio's lib/ffi pulls in filecoin-ffi (CGo + Rust toolchain), which
// breaks the pure-Go bundle promise. Curio's PDP tasks only call ONE
// method from that package — SealCalls.PieceReader, a storage-read path
// — so the carve-out is small: declare a single-method interface, wire
// a pure-Go implementation against the piece-park storage, and inject
// it into the PDP tasks where they expect *ffi.SealCalls.
//
// This file owns the interface. The pure-Go implementation lives in
// internal/pieceio/parkstore. The shim that adapts the interface back
// to Curio's expected *ffi.SealCalls shape (via a structurally-compatible
// stand-in struct) lives in internal/pieceio/sealcalls-shim, landed
// when the PDP integration begins.

package pieceio

import (
	"context"
	"io"

	"github.com/ipfs/go-cid"
)

// PieceReader returns a reader for a piece's bytes by piece CID.
// Implementations look up the piece in their backing storage and
// return an io.ReadCloser the caller must close.
//
// Errors:
//   - ErrPieceNotFound if the piece is not held locally
//   - ctx errors propagated as-is
//   - other errors wrap underlying I/O issues
type PieceReader interface {
	PieceReader(ctx context.Context, pieceCID cid.Cid) (io.ReadCloser, error)
}

// ErrPieceNotFound is returned by PieceReader implementations when the
// requested piece is not in local storage.
var ErrPieceNotFound = errPieceNotFound{}

type errPieceNotFound struct{}

func (errPieceNotFound) Error() string { return "piece not found in local storage" }
