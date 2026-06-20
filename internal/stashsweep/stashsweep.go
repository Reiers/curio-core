// Package stashsweep provides the stash integrity + garbage-collection
// safety net for curio-core's single-node hot-storage shape, where the
// diskstash file IS the piece's long-term storage (see
// internal/parkcomplete for the rationale).
//
// Two distinct hazards, handled by one periodic harmonytask because they
// share the same scan (list stash files + build the referenced-path set):
//
//   - INTEGRITY (#89): a parked_pieces row says complete=1 but its stash
//     file is gone. Proving that piece will FAULT on-chain (real penalty
//     on mainnet). We detect it and stamp parked_pieces.integrity_missing_at
//     so doctor / dashboard / alerts surface it instead of discovering the
//     loss at challenge time. We never delete the row (the dataset record
//     is the operator's to resolve).
//
//   - GC (#90): a stash file on disk that NO parked_piece_refs row points
//     at (custore scheme) is an orphan — abandoned upload, never-finalized
//     piece, etc. Left forever it fills the disk and the SP stops proving
//     (a self-inflicted outage we already hit 2026-06-16). We reclaim
//     orphans older than a retention floor. We NEVER delete a file that
//     backs ANY parked_piece (referenced files are off-limits; a missing
//     file for a complete row is the integrity case, not GC).
//
// No chain RPC, no FFI: pure SQLite + filesystem. Singleton, slow cadence.
package stashsweep

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	logging "github.com/ipfs/go-log/v2"

	"github.com/Reiers/curio-core/internal/parkcomplete"
)

var log = logging.Logger("curio-core/stashsweep")

// Config tunes the sweep. Zero value is safe (sensible defaults applied).
type Config struct {
	// StashDir is the diskstash directory.
	StashDir string

	// PollInterval is how often the sweep runs. Default 1h.
	PollInterval time.Duration

	// GCEnabled arms deletion of orphan files. When false, the sweep
	// still runs integrity + reports GC candidates (dry-run) but deletes
	// nothing. Default: integrity always runs; GC defaults to dry-run
	// unless explicitly enabled.
	GCEnabled bool

	// GCRetention is the minimum age a file must reach before GC will
	// reclaim it, so an in-flight streaming upload (.tmp mid-write) is
	// never raced. Default 24h.
	GCRetention time.Duration

	// AlertFn, if non-nil, surfaces an operator-visible alert. Called
	// with (source, message, contextFields). Best-effort; must not block.
	AlertFn func(ctx context.Context, source, message string, fields map[string]any)
}

func (c *Config) withDefaults() {
	if c.PollInterval <= 0 {
		c.PollInterval = time.Hour
	}
	if c.GCRetention <= 0 {
		c.GCRetention = 24 * time.Hour
	}
}

// Task is the harmonytask. Construct via New.
type Task struct {
	db  harmonyquery.DBInterface
	cfg Config
	now func() time.Time // injectable clock for tests
}

// New builds the sweep task.
func New(db harmonyquery.DBInterface, cfg Config) *Task {
	cfg.withDefaults()
	return &Task{db: db, cfg: cfg, now: time.Now}
}

// Result summarizes one sweep pass (returned for tests + logging).
type Result struct {
	CompleteChecked  int
	IntegrityBroken  int // complete=1 rows whose file is missing (newly or still)
	IntegrityHealed  int // rows whose file reappeared (flag cleared)
	OrphanCandidates int // unreferenced files older than retention
	OrphanReclaimed  int // actually deleted (0 when GC disabled)
	BytesReclaimed   int64
}

// Do runs one integrity + GC pass.
func (t *Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	res, err := t.sweep(ctx, stillOwned)
	if err != nil {
		return false, err
	}
	if res.IntegrityBroken > 0 || res.OrphanReclaimed > 0 || res.IntegrityHealed > 0 {
		log.Infow("stashsweep: pass complete",
			"complete_checked", res.CompleteChecked,
			"integrity_broken", res.IntegrityBroken,
			"integrity_healed", res.IntegrityHealed,
			"orphan_candidates", res.OrphanCandidates,
			"orphan_reclaimed", res.OrphanReclaimed,
			"bytes_reclaimed", res.BytesReclaimed,
			"gc_enabled", t.cfg.GCEnabled)
	}
	return true, nil
}

// sweep is the testable core.
func (t *Task) sweep(ctx context.Context, stillOwned func() bool) (Result, error) {
	var res Result

	// --- Build the referenced-path set (custore scheme only) ---
	// Every stash path any parked_piece_refs row points at. Files in this
	// set are NEVER GC candidates.
	var refs []struct {
		DataURL string `db:"data_url"`
	}
	if err := t.db.SelectI(ctx, &refs,
		`SELECT data_url FROM parked_piece_refs WHERE data_url LIKE 'custore://%'`); err != nil {
		return res, fmt.Errorf("stashsweep: select refs: %w", err)
	}
	referenced := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		if p, err := parkcomplete.StashPathFromCustoreURL(r.DataURL, t.cfg.StashDir); err == nil {
			referenced[p] = struct{}{}
		}
	}

	// --- INTEGRITY: complete=1 rows with a custore ref, stat the file ---
	if err := t.integrityPass(ctx, stillOwned, &res); err != nil {
		return res, err
	}
	if !stillOwned() {
		return res, nil
	}

	// --- GC: stash files not in the referenced set, older than retention ---
	if err := t.gcPass(ctx, stillOwned, referenced, &res); err != nil {
		return res, err
	}
	return res, nil
}

// integrityPass stats the file behind each complete=1 piece and toggles
// parked_pieces.integrity_missing_at accordingly.
func (t *Task) integrityPass(ctx context.Context, stillOwned func() bool, res *Result) error {
	var rows []struct {
		PieceID   int64   `db:"piece_id"`
		DataURL   string  `db:"data_url"`
		MissingAt *string `db:"integrity_missing_at"`
	}
	// One ref per piece is enough to locate the file; MIN(data_url) is
	// deterministic. complete=1 only — incomplete pieces aren't expected
	// to have stable bytes yet.
	if err := t.db.SelectI(ctx, &rows, `
		SELECT pp.id AS piece_id,
		       MIN(pr.data_url) AS data_url,
		       pp.integrity_missing_at AS integrity_missing_at
		FROM parked_pieces AS pp
		JOIN parked_piece_refs AS pr ON pr.piece_id = pp.id
		WHERE pp.complete = 1
		  AND pr.data_url LIKE 'custore://%'
		GROUP BY pp.id, pp.integrity_missing_at`); err != nil {
		return fmt.Errorf("stashsweep: select complete pieces: %w", err)
	}

	for _, r := range rows {
		if !stillOwned() {
			return nil
		}
		res.CompleteChecked++
		path, err := parkcomplete.StashPathFromCustoreURL(r.DataURL, t.cfg.StashDir)
		if err != nil {
			// malformed/out-of-stashdir URL: treat as broken-but-skip
			// (don't flip, just warn — it's a data-quality issue).
			log.Warnw("stashsweep: bad custore URL on complete piece",
				"piece_id", r.PieceID, "data_url", r.DataURL, "err", err)
			continue
		}
		_, statErr := os.Stat(path)
		missing := os.IsNotExist(statErr)
		switch {
		case missing && r.MissingAt == nil:
			// newly broken: stamp it
			ts := t.now().UTC().Format(time.RFC3339)
			if _, err := t.db.ExecI(ctx,
				`UPDATE parked_pieces SET integrity_missing_at = ? WHERE id = ? AND integrity_missing_at IS NULL`,
				ts, r.PieceID); err != nil {
				return fmt.Errorf("stashsweep: stamp broken piece %d: %w", r.PieceID, err)
			}
			res.IntegrityBroken++
			log.Warnw("stashsweep: complete piece is MISSING its stash file (will fault on prove)",
				"piece_id", r.PieceID, "stash_path", path)
			if t.cfg.AlertFn != nil {
				t.cfg.AlertFn(ctx, "stashsweep/integrity",
					fmt.Sprintf("parked_piece %d marked complete but stash file is gone; proving it will fault", r.PieceID),
					map[string]any{"piece_id": r.PieceID, "stash_path": path})
			}
		case missing && r.MissingAt != nil:
			// still broken (already stamped): count it, no write
			res.IntegrityBroken++
		case !missing && r.MissingAt != nil:
			// healed: file reappeared, clear the flag
			if _, err := t.db.ExecI(ctx,
				`UPDATE parked_pieces SET integrity_missing_at = NULL WHERE id = ? AND integrity_missing_at IS NOT NULL`,
				r.PieceID); err != nil {
				return fmt.Errorf("stashsweep: clear healed piece %d: %w", r.PieceID, err)
			}
			res.IntegrityHealed++
			log.Infow("stashsweep: previously-missing stash file reappeared, integrity flag cleared",
				"piece_id", r.PieceID, "stash_path", path)
		}
	}
	return nil
}

// gcPass lists stash files and reclaims orphans (no referencing ref row)
// older than the retention floor. When GCEnabled is false it only counts
// candidates (dry-run) and deletes nothing.
func (t *Task) gcPass(ctx context.Context, stillOwned func() bool, referenced map[string]struct{}, res *Result) error {
	entries, err := os.ReadDir(t.cfg.StashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no stash dir yet, nothing to GC
		}
		return fmt.Errorf("stashsweep: read stash dir: %w", err)
	}
	cutoff := t.now().Add(-t.cfg.GCRetention)

	for _, e := range entries {
		if !stillOwned() {
			return nil
		}
		if e.IsDir() {
			continue
		}
		// Only ever touch our own stash files.
		if !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		full := filepath.Join(t.cfg.StashDir, e.Name())
		if _, ok := referenced[full]; ok {
			continue // referenced => never a GC candidate
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue // too young; could be an in-flight upload
		}
		res.OrphanCandidates++
		if !t.cfg.GCEnabled {
			log.Debugw("stashsweep: GC candidate (dry-run, not deleting)",
				"file", full, "size", info.Size(), "age", t.now().Sub(info.ModTime()).String())
			continue
		}
		if err := os.Remove(full); err != nil {
			log.Warnw("stashsweep: failed to reclaim orphan", "file", full, "err", err)
			continue
		}
		res.OrphanReclaimed++
		res.BytesReclaimed += info.Size()
		log.Infow("stashsweep: reclaimed orphan stash file", "file", full, "size", info.Size())
	}
	if res.OrphanCandidates > 0 && !t.cfg.GCEnabled {
		log.Infow("stashsweep: GC dry-run found orphan candidates (GC disabled — set stash-gc.enabled to reclaim)",
			"candidates", res.OrphanCandidates)
	}
	return nil
}

// --- harmonytask plumbing ---

func (t *Task) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:         taskhelp.Max(1),
		Name:        "StashSweep",
		Cost:        resources.Resources{Cpu: 1, Ram: 32 << 20},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(t.cfg.PollInterval, t),
	}
}

func (t *Task) Adder(_ harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = (*Task)(nil)
var _ = harmonytask.Reg(&Task{cfg: Config{PollInterval: time.Hour}})
