package datapath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve_ExplicitWins(t *testing.T) {
	t.Setenv("CURIO_PDP_DATA", "/env/should/lose")
	got := Resolve("/explicit/path", "/fallback")
	if got.Path != "/explicit/path" || got.Source != "flag" {
		t.Fatalf("explicit flag should win: got %+v", got)
	}
}

func TestResolve_EnvPrecedence(t *testing.T) {
	// Make sure no real mount discovery interferes.
	withMounts(t, nil)

	t.Setenv("CURIO_PDP_DATA", "")
	t.Setenv("DATA_STORAGE", "")
	t.Setenv("CURIO_DATA", "")

	// CURIO_PDP_DATA is highest-priority env.
	t.Setenv("DATA_STORAGE", "/b")
	t.Setenv("CURIO_PDP_DATA", "/a")
	if got := Resolve("", "/fb"); got.Path != "/a" || got.Source != "env:CURIO_PDP_DATA" {
		t.Fatalf("CURIO_PDP_DATA should win: got %+v", got)
	}

	// With CURIO_PDP_DATA unset, DATA_STORAGE (compatibility alias) wins next.
	t.Setenv("CURIO_PDP_DATA", "")
	if got := Resolve("", "/fb"); got.Path != "/b" || got.Source != "env:DATA_STORAGE" {
		t.Fatalf("DATA_STORAGE should win: got %+v", got)
	}
}

func TestResolve_FallbackWhenNothingFound(t *testing.T) {
	clearEnv(t)
	withMounts(t, []string{t.TempDir()}) // empty mount, no label dir
	if got := Resolve("", "/fb/stash"); got.Path != "/fb/stash" || got.Source != "fallback" {
		t.Fatalf("expected fallback: got %+v", got)
	}
}

func TestDiscover_LabelledFolderAtRoot(t *testing.T) {
	clearEnv(t)
	mnt := t.TempDir()
	want := filepath.Join(mnt, LabelDir)
	mustMkdir(t, want)
	withMounts(t, []string{mnt})

	got := Resolve("", "/fb")
	if got.Source != "discovered" {
		t.Fatalf("source = %q, want discovered (%+v)", got.Source, got)
	}
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
}

func TestDiscover_NestedWithinDepth(t *testing.T) {
	clearEnv(t)
	mnt := t.TempDir()
	// depth 2: <mnt>/disk1/curio-pdp-data
	want := filepath.Join(mnt, "disk1", LabelDir)
	mustMkdir(t, want)
	withMounts(t, []string{mnt})

	if got := Resolve("", "/fb"); got.Path != want {
		t.Fatalf("nested discovery failed: got %+v want %q", got, want)
	}
}

func TestDiscover_TooDeepIsIgnored(t *testing.T) {
	clearEnv(t)
	mnt := t.TempDir()
	// depth 4 (> MaxDepth=3): a/b/c/curio-pdp-data
	tooDeep := filepath.Join(mnt, "a", "b", "c", LabelDir)
	mustMkdir(t, tooDeep)
	withMounts(t, []string{mnt})

	if got := Resolve("", "/fb"); got.Source != "fallback" {
		t.Fatalf("too-deep label should be ignored: got %+v", got)
	}
}

func TestDiscover_PrefersMostFreeSpace(t *testing.T) {
	clearEnv(t)
	mntSmall := t.TempDir()
	mntBig := t.TempDir()
	small := filepath.Join(mntSmall, LabelDir)
	big := filepath.Join(mntBig, LabelDir)
	mustMkdir(t, small)
	mustMkdir(t, big)
	withMounts(t, []string{mntSmall, mntBig})

	// Fake free space: big mount has more.
	oldFree := freeSpacer
	t.Cleanup(func() { freeSpacer = oldFree })
	freeSpacer = func(p string) uint64 {
		if p == big {
			return 1 << 40 // 1 TiB
		}
		return 1 << 20 // 1 MiB
	}

	if got := Resolve("", "/fb"); got.Path != big {
		t.Fatalf("expected most-free-space mount %q, got %+v", big, got)
	}
}

func TestEnsureDir_CreatesAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", LabelDir)
	p, err := EnsureDir(dir)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if p != dir {
		t.Fatalf("EnsureDir returned %q want %q", p, dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
	// idempotent
	if _, err := EnsureDir(dir); err != nil {
		t.Fatalf("second EnsureDir: %v", err)
	}
}

// --- helpers ---

func withMounts(t *testing.T, mounts []string) {
	t.Helper()
	old := mountLister
	t.Cleanup(func() { mountLister = old })
	mountLister = func() []string { return mounts }
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, e := range envVars {
		t.Setenv(e, "")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}
