package pieceio_test

import (
	"errors"
	"testing"

	"github.com/Reiers/curio-core/internal/pieceio"
)

// TestErrPieceNotFound: the sentinel must be comparable via errors.Is
// so callers can branch cleanly on "not held locally" without string
// matching.
func TestErrPieceNotFound(t *testing.T) {
	// errors.Is against the exported sentinel returns true for itself.
	if !errors.Is(pieceio.ErrPieceNotFound, pieceio.ErrPieceNotFound) {
		t.Error("ErrPieceNotFound is not Is-comparable to itself")
	}
	// Surface error message is operator-friendly.
	if msg := pieceio.ErrPieceNotFound.Error(); msg == "" || len(msg) > 200 {
		t.Errorf("error message length out of bounds: %q", msg)
	}
}
