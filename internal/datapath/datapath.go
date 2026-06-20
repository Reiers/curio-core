// Package datapath resolves where curio-core stores piece data (the
// diskstash). It implements zero-config auto-discovery so a fresh
// operator can just create a folder named "curio-pdp-data" on any
// mounted disk and curio-core will find and use it — no flags, no
// config file, no DB host.
//
// This is the "it just works" onboarding touch: parity with (and a
// superset of) the labelled-folder discovery described for Curio's
// Skiff build, except here it is actually implemented and tested,
// CGO-free, and works on linux + darwin.
//
// Resolution precedence (first hit wins):
//
//  1. explicit path        — the --data-storage flag / Resolve(explicit=...)
//  2. env CURIO_PDP_DATA   — primary env override
//  3. env DATA_STORAGE     — Skiff-compatible alias
//  4. env CURIO_DATA       — Skiff-compatible alias
//  5. labelled folder      — a directory named LabelDir ("curio-pdp-data")
//     found within MaxDepth levels of any real mountpoint, preferring
//     the mount with the most free space (so big data disks win over /)
//  6. fallback             — <dataDir>/stash (the historical default)
//
// Discovery never creates the labelled folder for the operator; the
// folder's existence is the opt-in signal. The chosen path is created
// (MkdirAll) only once selected, so the daemon can write into it.
package datapath

import (
	"os"
	"path/filepath"
	"sort"
)

// LabelDir is the directory name an operator creates to mark a disk as
// curio-core data storage. Matches the documented onboarding step.
const LabelDir = "curio-pdp-data"

// MaxDepth is how many directory levels below a mountpoint we descend
// looking for a LabelDir. Kept shallow so discovery is fast and can't
// wander into deep trees.
const MaxDepth = 3

// Env var names checked, in order. The first non-empty wins.
var envVars = []string{"CURIO_PDP_DATA", "DATA_STORAGE", "CURIO_DATA"}

// Result describes the resolved data path and how it was chosen, so the
// daemon can log a clear one-liner at boot.
type Result struct {
	Path   string // absolute path that will hold piece data
	Source string // "flag" | "env:<NAME>" | "discovered" | "fallback"
}

// mountLister is swapped in tests. In production it returns real
// mountpoints (see mounts_*.go per-GOOS).
var mountLister = listMounts

// freeSpacer is swapped in tests. Returns bytes free on the filesystem
// containing path, or 0 if it can't tell.
var freeSpacer = freeBytes

// Resolve picks the data path. explicit is the value of the
// --data-storage flag ("" if unset). fallback is the historical default
// (typically <dataDir>/stash). The returned Path is NOT yet created;
// call EnsureDir on it.
func Resolve(explicit, fallback string) Result {
	if explicit != "" {
		return Result{Path: explicit, Source: "flag"}
	}
	for _, name := range envVars {
		if v := os.Getenv(name); v != "" {
			return Result{Path: v, Source: "env:" + name}
		}
	}
	if p := discoverLabelled(mountLister(), MaxDepth); p != "" {
		return Result{Path: p, Source: "discovered"}
	}
	return Result{Path: fallback, Source: "fallback"}
}

// discoverLabelled scans each mountpoint for a LabelDir within maxDepth
// levels. When multiple are found, the one on the filesystem with the
// most free space wins (big data disks beat the OS root). Deterministic:
// ties break by shortest path then lexical order.
func discoverLabelled(mounts []string, maxDepth int) string {
	type cand struct {
		path string
		free uint64
	}
	var found []cand
	seen := map[string]bool{}

	for _, m := range mounts {
		for _, hit := range findLabelDirs(m, maxDepth) {
			abs, err := filepath.Abs(hit)
			if err != nil {
				abs = hit
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			found = append(found, cand{path: abs, free: freeSpacer(abs)})
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].free != found[j].free {
			return found[i].free > found[j].free // most free space first
		}
		if len(found[i].path) != len(found[j].path) {
			return len(found[i].path) < len(found[j].path)
		}
		return found[i].path < found[j].path
	})
	return found[0].path
}

// findLabelDirs walks up to maxDepth levels below root, returning every
// directory whose base name is LabelDir. A LabelDir directly at root
// counts as depth 1. Symlinks are not followed (avoids loops + escapes).
func findLabelDirs(root string, maxDepth int) []string {
	var out []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
				continue
			}
			child := filepath.Join(dir, e.Name())
			if e.Name() == LabelDir {
				out = append(out, child)
				// don't descend into a matched data dir
				continue
			}
			walk(child, depth+1)
		}
	}
	walk(root, 1)
	return out
}

// EnsureDir creates the resolved path (and parents) with 0o750 perms,
// returning the path so callers can chain. It is safe to call on an
// existing directory.
func EnsureDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", err
	}
	return path, nil
}
