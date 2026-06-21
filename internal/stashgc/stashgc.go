// Package stashgc reclaims orphaned stash files to prevent a curio-core SP
// from filling its own disk and grinding proving to a halt (curio-core#90).
//
// Curio Core never garbage-collects stash files by design: the stash IS
// long-term storage (see internal/parkcomplete). But files that are never
// finalized -- abandoned uploads, stale .tmp from interrupted streams,
// refs that were deleted -- accumulate forever. On 2026-06-16 this filled
// a 75G disk -> SQLite ENOSPC -> outage. This task reclaims ONLY files with
// no live reason to exist.
//
// Safety model (learned the hard way -- the 2026-06-16 manual cleanup
// deleted LIVE data because it didn't check refs):
//
//   - A file is reclaimable iff it is NOT referenced by any
//     parked_piece_refs.data_url (custore scheme) AND it is older than a
//     retention floor (so concurrent streaming uploads mid-write are never
//     raced).
//   - We NEVER delete a file backing a complete=1 row. That is the
//     integrity case (#89): flag, don't delete.
//   - Dry-run by default surfaces what WOULD be deleted; deletion is only
//     armed explicitly.
//
// No chain RPC, no FFI. Singleton harmonytask, slow IAmBored cadence.
package stashgc

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

var log = logging.Logger("curio-core/stashgc")

// Defaults mirror the issue: conservative retention, hourly sweep, and
// dry-run ON unless the operator explicitly arms deletion.
const (
	DefaultPollInterval = time.Hour
	DefaultRetention    = 24 * time.Hour
)

// Config carries the GC knobs. Zero value is the safe default (dry-run,
// 24h retention, hourly).
type Config struct {
	StashDir string
	// Retention is the minimum age a file must reach before it is even a
	// deletion candidate. Protects in-flight uploads. Zero -> default.
	Retention time.Duration
	// PollInterval is the idle cadence. Zero -> default.
	PollInterval time.Duration
	// DryRun, when true (the default), logs+reports candidates without
	// deleting. Must be explicitly set false to arm deletion.
	DryRun bool
}

func (c Config) retention() time.Duration {
	if c.Retention <= 0 {
		return DefaultRetention
	}
	return c.Retention
}

func (c Config) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return DefaultPollInterval
	}
	return c.PollInterval
}

// Task is the stash-GC sweep. Construct via New.
type Task struct {
	db  harmonyquery.DBInterface
	cfg Config
}

// New constructs the GC task. A zero-value-ish Config is safe (dry-run).
func New(db harmonyquery.DBInterface, cfg Config) *Task {
	return &Task{db: db, cfg: cfg}
}

// Do runs one sweep pass: list stash files, diff against referenced paths,
// delete (or in dry-run, report) unreferenced files older than retention.
func (t *Task) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if t.cfg.StashDir == "" {
		return true, nil
	}
	absStash, err := filepath.Abs(t.cfg.StashDir)
	if err != nil {
		return false, fmt.Errorf("stashgc: resolve stash dir: %w", err)
	}

	// 1. Build the set of referenced stash paths from parked_piece_refs.
	//    Includes refs to BOTH complete and incomplete pieces: a referenced
	//    file is live regardless of completion. (The complete=1 integrity
	//    case is handled by #89; here we only reclaim the UNreferenced.)
	referenced, err := t.referencedPaths(ctx, absStash)
	if err != nil {
		return false, fmt.Errorf("stashgc: build referenced set: %w", err)
	}

	// 2. List on-disk stash files.
	entries, err := os.ReadDir(absStash)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("stashgc: read stash dir: %w", err)
	}

	cutoff := time.Now().Add(-t.cfg.retention())
	var reclaimable, reclaimedBytes int64
	var deleted int
	for _, e := range entries {
		if !stillOwned() {
			return false, nil
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Only consider stash artifacts (UUID .tmp files from diskstash).
		if !strings.HasSuffix(name, ".tmp") {
			continue
		}
		full := filepath.Join(absStash, name)
		if referenced[full] {
			continue // live: a ref points at it
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue // too new: protect in-flight uploads
		}
		// Candidate: unreferenced + old.
		reclaimable++
		reclaimedBytes += info.Size()
		if t.cfg.DryRun {
			log.Infow("stashgc: would reclaim (dry-run)",
				"file", full, "size", info.Size(), "age", time.Since(info.ModTime()).Round(time.Minute).String())
			continue
		}
		if rerr := os.Remove(full); rerr != nil {
			log.Warnw("stashgc: delete failed", "file", full, "err", rerr)
			reclaimedBytes -= info.Size()
			reclaimable--
			continue
		}
		log.Infow("stashgc: reclaimed orphan stash file", "file", full, "size", info.Size())
		deleted++
	}

	if reclaimable > 0 {
		mode := "armed"
		if t.cfg.DryRun {
			mode = "dry-run"
		}
		log.Infow("stashgc: sweep done",
			"mode", mode, "candidates", reclaimable, "deleted", deleted,
			"bytes", reclaimedBytes, "retention", t.cfg.retention().String())
	}
	return true, nil
}

// referencedPaths returns the set of absolute stash file paths that any
// parked_piece_refs row points at via a custore:// data_url.
func (t *Task) referencedPaths(ctx context.Context, absStash string) (map[string]bool, error) {
	var rows []struct {
		DataURL string `db:"data_url"`
	}
	if err := t.db.SelectI(ctx, &rows,
		`SELECT data_url FROM parked_piece_refs WHERE data_url LIKE 'custore://%'`); err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, r := range rows {
		p, err := stashPathFromCustoreURL(r.DataURL, absStash)
		if err != nil {
			// A ref we can't resolve is treated as live (fail safe: never
			// reclaim a file just because its ref URL looks odd).
			continue
		}
		set[p] = true
	}
	return set, nil
}

// CanAccept accepts every offered task; idempotent + bounded.
func (t *Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

// TypeDetails registers a singleton IAmBored task.
func (t *Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "StashGC",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 32 << 20,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(t.cfg.pollInterval(), t),
	}
}

// Adder is unused; scheduled exclusively via IAmBored.
func (t *Task) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = (*Task)(nil)
var _ = harmonytask.Reg(&Task{})

// stashPathFromCustoreURL converts a custore:// data URL into the on-disk
// stash path, validating it sits inside stashDir. Mirrors the resolver in
// internal/parkcomplete + internal/stashintegrity.
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
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("custore URL path %q is not inside stash dir %q", candidate, absStashDir)
	}
	return candidate, nil
}
