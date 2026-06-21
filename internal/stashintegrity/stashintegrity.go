// Package stashintegrity provides a periodic safety-net sweep that detects
// parked_pieces rows marked complete=1 whose backing stash file has gone
// missing, BEFORE the proving loop faults on them (curio-core#89).
//
// Why this exists: in curio-core's single-node shape, the stash file IS
// long-term storage -- the bytes never move (see internal/parkcomplete).
// parkcomplete checks file-exists at the instant it flips complete 0->1.
// After that, nothing re-checks. If a complete=1 piece's stash file later
// vanishes (operator wiped the dir, a stale-.tmp cleanup deleted live data,
// disk corruption), the gap is invisible until a proving window challenges
// that piece and ProveTask faults. On mainnet a failed proof is a fault
// against the dataset = real economic penalty.
//
// This task:
//  1. For each parked_pieces row with complete=1 referenced by a
//     custore:// data_url, resolve the stash path and os.Stat() it.
//  2. On a missing file: set parked_pieces.integrity_missing_at (if not
//     already set) and emit a deduplicated operator alert.
//  3. On a file that reappears: clear integrity_missing_at and let the alert
//     age out.
//
// Mark, never delete: deleting the row could cascade
// (parked_piece_refs ON DELETE CASCADE) and remove the dataset's only
// record. GC of truly-unreferenced files is the sibling task (stashgc, #90).
//
// No chain RPC, no FFI. Singleton harmonytask, fires via IAmBored on a slow
// cadence -- this is a safety net, not a hot path.
package stashintegrity

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	logging "github.com/ipfs/go-log/v2"

	"github.com/Reiers/curio-core/internal/alerts"
)

var log = logging.Logger("curio-core/stashintegrity")

// PollInterval is how often the sweep runs when idle. Slow on purpose:
// a safety net, not a hot path.
const PollInterval = 10 * time.Minute

// batchSize bounds the work per invocation so a large catalog never
// monopolizes a scheduler slot. The sweep is resumable: the next tick
// picks up where this one left off (ORDER BY id, bounded LIMIT).
const batchSize = 256

// Task is the stash-integrity sweep. Construct via New.
type Task struct {
	db       harmonyquery.DBInterface
	stashDir string
}

// New constructs the sweep bound to the diskstash directory.
func New(db harmonyquery.DBInterface, stashDir string) *Task {
	return &Task{db: db, stashDir: stashDir}
}

type candidate struct {
	PieceID        int64  `db:"piece_id"`
	DataURL        string `db:"data_url"`
	AlreadyMissing int64  `db:"already_missing"`
}

// Do runs one bounded sweep pass.
func (t *Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var candidates []candidate
	err = t.db.SelectI(ctx, &candidates, `
		SELECT pp.id AS piece_id,
		       pr.data_url AS data_url,
		       CASE WHEN pp.integrity_missing_at IS NOT NULL THEN 1 ELSE 0 END AS already_missing
		FROM parked_pieces AS pp
		JOIN parked_piece_refs AS pr ON pr.piece_id = pp.id
		WHERE pp.complete = 1
		  AND pr.data_url LIKE 'custore://%'
		ORDER BY pp.id ASC
		LIMIT ?`, batchSize)
	if err != nil {
		return false, fmt.Errorf("stashintegrity: select candidates: %w", err)
	}
	if len(candidates) == 0 {
		return true, nil
	}

	var newlyMissing, recovered int
	for _, c := range candidates {
		if !stillOwned() {
			return false, nil
		}
		stashPath, perr := stashPathFromCustoreURL(c.DataURL, t.stashDir)
		if perr != nil {
			// A bad custore URL is itself a kind of integrity problem,
			// but it's not a missing-file case; log and move on.
			log.Warnw("stashintegrity: skip (bad custore URL)",
				"piece_id", c.PieceID, "data_url", c.DataURL, "err", perr)
			continue
		}

		_, serr := os.Stat(stashPath)
		fileMissing := serr != nil && os.IsNotExist(serr)
		if serr != nil && !os.IsNotExist(serr) {
			// Transient stat error (permission, I/O): don't flip state
			// on an ambiguous signal.
			log.Warnw("stashintegrity: stat error (ignored this pass)",
				"piece_id", c.PieceID, "stash_path", stashPath, "err", serr)
			continue
		}

		switch {
		case fileMissing && c.AlreadyMissing == 0:
			// First detection: mark + alert.
			if _, uerr := t.db.ExecI(ctx,
				`UPDATE parked_pieces SET integrity_missing_at = ?
				 WHERE id = ? AND integrity_missing_at IS NULL`,
				time.Now().UTC().Format(time.RFC3339), c.PieceID); uerr != nil {
				return false, fmt.Errorf("stashintegrity: mark piece %d: %w", c.PieceID, uerr)
			}
			log.Errorw("stashintegrity: complete piece has NO stash file (latent proof fault)",
				"piece_id", c.PieceID, "stash_path", stashPath)
			_, _ = alerts.Emit(ctx, t.db, alerts.EmitArgs{
				Severity:    alerts.SeverityError,
				Source:      "stashintegrity/missing-file",
				Message:     fmt.Sprintf("complete piece %d has no backing stash file (latent proof fault)", c.PieceID),
				Fingerprint: fmt.Sprintf("stashintegrity/missing/%d", c.PieceID),
				Context: map[string]any{
					"pieceId":   c.PieceID,
					"stashPath": stashPath,
				},
			})
			newlyMissing++

		case !fileMissing && c.AlreadyMissing == 1:
			// File reappeared: clear the flag. The alert ages out on its
			// own (no new emit refreshes its last_seen_at).
			if _, uerr := t.db.ExecI(ctx,
				`UPDATE parked_pieces SET integrity_missing_at = NULL WHERE id = ?`,
				c.PieceID); uerr != nil {
				return false, fmt.Errorf("stashintegrity: clear piece %d: %w", c.PieceID, uerr)
			}
			log.Infow("stashintegrity: stash file recovered; cleared missing flag",
				"piece_id", c.PieceID, "stash_path", stashPath)
			recovered++
		}
	}

	if newlyMissing > 0 || recovered > 0 {
		log.Infow("stashintegrity: sweep batch done",
			"checked", len(candidates), "newly_missing", newlyMissing, "recovered", recovered)
	}
	return true, nil
}

// CanAccept accepts every offered task; the work is idempotent + bounded.
func (t *Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

// TypeDetails registers a singleton IAmBored task. Cheap: SQLite reads +
// os.Stat calls, no chain RPC, no FFI.
func (t *Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "StashIntegrity",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 32 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(PollInterval, t),
	}
}

// Adder is unused; scheduled exclusively via IAmBored.
func (t *Task) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = (*Task)(nil)
var _ = harmonytask.Reg(&Task{})

// CountMissing returns the number of complete pieces currently flagged as
// missing their stash file. For doctor + the dashboard storage page.
func CountMissing(ctx context.Context, db harmonyquery.DBInterface) (int64, error) {
	var n int64
	err := db.QueryRowI(ctx,
		`SELECT COUNT(*) FROM parked_pieces WHERE integrity_missing_at IS NOT NULL`).Scan(&n)
	return n, err
}

// stashPathFromCustoreURL converts a custore:// data URL into the on-disk
// stash path, validating that it sits inside stashDir. Mirrors the resolver
// in internal/parkcomplete (kept local to avoid a cross-package export).
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
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 2 && rel[0] == '.' && rel[1] == '.' {
		return "", fmt.Errorf("custore URL path %q is not inside stash dir %q", candidate, absStashDir)
	}
	return candidate, nil
}
