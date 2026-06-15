// Package parkcomplete provides the streaming-upload-to-parked-piece
// completion bridge for curio-core's pdpv0-only deployment shape.
//
// Why this exists: upstream Curio's piece-upload pipeline has two
// steps. The streaming upload (POST /pdp/piece/uploads + PUT) lands
// bytes into a stash file and writes a `parked_pieces` row with
// `complete=FALSE`. Later, `tasks/piece.ParkPieceTask` copies the
// bytes from stash into long-term cluster storage (via the heavy
// paths.Remote / ffi.SealCalls multi-worker abstraction) and sets
// `complete=TRUE`.
//
// Curio Core skips that copy entirely: stash IS the long-term storage
// in our single-node shape. The bytes never move; the diskstash path
// IS where the piece reader will read from. So ParkPieceTask's
// complete-flip is the only thing missing for our pipeline.
//
// This package ships a minimal harmonytask that:
//
//  1. Polls for parked_pieces rows where complete=0 AND long_term=TRUE
//     AND a parked_piece_refs row references them (= the streaming
//     upload's finalize transaction committed)
//  2. Optionally verifies the underlying stash file exists on disk
//     (safety net against a manually-corrupted DB / wiped stash dir)
//  3. Flips complete=TRUE
//
// Once complete=TRUE, the downstream pipeline (PDPNotifyTask,
// SaveCache, ProveTask) sees the piece as available.

package parkcomplete

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio-core/parkcomplete")

// Task is the harmonytask that flips parked_pieces.complete=TRUE for
// streaming-upload-driven entries. Construct via New.
//
// Singleton (Max=1), fires every PollInterval via IAmBored. Reads +
// writes only SQLite tables curio-core's pdpv0 schema already has;
// no chain RPCs, no on-chain writes, no FFI.
type Task struct {
	db harmonyquery.DBInterface

	// stashDir is the directory diskstash uses for staging + long-term
	// storage. Used by the safety-net file-exists check before flipping
	// a piece's complete bit.
	stashDir string

	// onComplete, if non-nil, is called once per batch after at least one
	// parked_pieces row was flipped to complete=TRUE. In single-binary
	// curio-core this wakes PDPv0_Notify (via notify.Kick) inline so the
	// finalize task fires within ~ms instead of waiting for the Notify
	// poll cycle — the wake-at-write optimization (curio-core#67). nil is
	// safe (the Notify ticker poll remains the fallback).
	onComplete func()
}

// PollInterval is how often the task fires when there are no
// outstanding completions to process. Short because the streaming-
// upload finalize is synchronous from the client's perspective and
// clients expect downstream pipeline activation within seconds.
const PollInterval = 5 * time.Second

// New constructs the completion task. stashDir is the path diskstash
// uses (typically <data-dir>/stash); used for the file-exists safety
// net.
func New(db harmonyquery.DBInterface, stashDir string) *Task {
	return &Task{db: db, stashDir: stashDir}
}

// NewWithWake is New plus an onComplete callback fired after a batch
// flips at least one piece to complete. curio-core passes notify.Kick
// here so the PDPv0_Notify finalize task wakes immediately instead of
// waiting for its own poll cycle (curio-core#67). onComplete must be
// non-blocking and safe to call from the task goroutine.
func NewWithWake(db harmonyquery.DBInterface, stashDir string, onComplete func()) *Task {
	return &Task{db: db, stashDir: stashDir, onComplete: onComplete}
}

// Do is the harmonytask body. Processes up to BatchSize candidate
// completions per invocation.
func (t *Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	const batchSize = 32

	// Candidates: parked_pieces rows that are
	//   - incomplete (complete = 0)
	//   - long-term (long_term = 1)
	//   - referenced by at least one parked_piece_refs row whose
	//     data_url scheme is 'custore' (= written by the streaming-
	//     upload path)
	//
	// The custore-scheme filter is the safety against double-completing
	// rows that some other code path (e.g. a future ParkPieceTask
	// adoption) is responsible for completing.
	var candidates []struct {
		PieceID int64  `db:"piece_id"`
		DataURL string `db:"data_url"`
	}
	err = t.db.SelectI(ctx, &candidates, `
		SELECT pp.id AS piece_id, pr.data_url
		FROM parked_pieces AS pp
		JOIN parked_piece_refs AS pr ON pr.piece_id = pp.id
		WHERE pp.complete = 0
		  AND pp.long_term = 1
		  AND pr.data_url LIKE 'custore://%'
		ORDER BY pp.id ASC
		LIMIT ?`, batchSize)
	if err != nil {
		return false, fmt.Errorf("parkcomplete: select candidates: %w", err)
	}
	if len(candidates) == 0 {
		return true, nil
	}

	completed := 0
	for _, c := range candidates {
		if !stillOwned() {
			return false, nil
		}
		// Verify the stash file exists before flipping. Belt-and-braces
		// against a DB referencing a stash UUID that's gone (operator
		// wiped the stash dir, file got rm'd, etc).
		stashPath, err := stashPathFromCustoreURL(c.DataURL, t.stashDir)
		if err != nil {
			log.Warnw("parkcomplete: skip candidate (bad custore URL)",
				"piece_id", c.PieceID, "data_url", c.DataURL, "err", err)
			continue
		}
		if _, err := os.Stat(stashPath); err != nil {
			if os.IsNotExist(err) {
				log.Warnw("parkcomplete: skip candidate (stash file absent)",
					"piece_id", c.PieceID, "stash_path", stashPath)
				continue
			}
			log.Warnw("parkcomplete: skip candidate (stash stat error)",
				"piece_id", c.PieceID, "stash_path", stashPath, "err", err)
			continue
		}

		_, err = t.db.ExecI(ctx,
			`UPDATE parked_pieces SET complete = 1 WHERE id = ? AND complete = 0`,
			c.PieceID)
		if err != nil {
			return false, fmt.Errorf("parkcomplete: flip complete for piece %d: %w", c.PieceID, err)
		}
		log.Infow("parkcomplete: marked parked_pieces.complete=TRUE",
			"piece_id", c.PieceID, "stash_path", stashPath)
		completed++
	}

	if completed > 0 {
		log.Infow("parkcomplete: batch done", "completed", completed, "candidates", len(candidates))
		// Wake-at-write (curio-core#67): a piece just became complete, so
		// kick PDPv0_Notify to scan now instead of waiting for its poll
		// tick. Single-binary only: same process owns both the write and
		// the Notify poll loop, so this is a pure in-process accelerator.
		if t.onComplete != nil {
			t.onComplete()
		}
	}
	return true, nil
}

// CanAccept accepts every task the scheduler offers. The work is
// idempotent and bounded; no resource-aware filtering needed.
func (t *Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

// TypeDetails registers this as a singleton task that fires every
// PollInterval via IAmBored. Cheap to run; no chain calls, just two
// SQLite queries per cycle.
func (t *Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "ParkComplete",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 32 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(PollInterval, t),
	}
}

// Adder is unused; tasks are scheduled exclusively via IAmBored.
func (t *Task) Adder(taskFunc harmonytask.AddTaskFunc) {}

// Compile-time + runtime guards: ParkComplete is a TaskInterface that
// the harmonytask registry must know about. Reg registers the type's
// name during package init so the scheduler doesn't reject Start with
// 'task ParkComplete not registered'.
var _ harmonytask.TaskInterface = (*Task)(nil)
var _ = harmonytask.Reg(&Task{})

// stashPathFromCustoreURL converts a custore:// data URL into the
// on-disk stash path. The URL is constructed by diskstash.StashURL
// with the scheme rewritten to custore by pdp/handlers_upload.go's
// handleStreamingUpload:
//
//	diskstash:  file:///var/lib/curio-core-demo/stash/<uuid>.tmp
//	custored:   custore:///var/lib/curio-core-demo/stash/<uuid>.tmp
//
// The URL Path component IS the full absolute filesystem path; no
// joining with stashDir is needed. stashDir is kept as a parameter
// for a sanity-check: if the URL path doesn't sit inside stashDir,
// the candidate is rejected (defense against malformed or attacker-
// supplied data_url values).
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
	// The path is absolute (starts with /). Sanity-check: it must sit
	// inside stashDir to be considered safe.
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
