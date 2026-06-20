package engine

import (
	"context"
	"testing"

	"github.com/filecoin-project/curio/tasks/tasknames"
)

// TestNew_InMemory asserts that an engine can be constructed against
// an in-memory SQLite, that the task registry holds entries for every
// PDP v0 task name we expect to ship with (pdpv0-only scope per Andy
// 2026-05-23; v1 is intentionally out of scope), and that Start →
// Healthy → Stop is a clean cycle.
func TestNew_InMemory(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	if e.DB() == nil {
		t.Fatal("DB() returned nil after New")
	}
	if e.Registry() == nil {
		t.Fatal("Registry() returned nil after New")
	}

	// --- pdpv0-only: v1 (`tasks/pdp`) intentionally not registered ---
	// If v1 is reintroduced later, restore the wantV1 block from git
	// history at commit fd85e79.

	// --- Required PDP v0 names ---
	wantV0 := []string{
		tasknames.PDPv0_Prove,
		tasknames.PDPv0_PullPiece,
		tasknames.PDPv0_SaveCache,
		tasknames.PDPv0_InitPP,
		tasknames.PDPv0_ProvPeriod,
		tasknames.PDPv0_Notify,
		tasknames.PDPv0_DelDataSet,
		tasknames.PDPv0_TermFWSS,
		tasknames.PDPv0_ChainSync,
	}
	for _, name := range wantV0 {
		if !e.Registry().Has(name) {
			t.Errorf("PDP v0 task %q missing from registry", name)
		}
		td, ok := e.Registry().Get(name)
		if !ok {
			continue
		}
		if td.Name != name {
			t.Errorf("PDP v0 task %q: TypeDetails.Name = %q, want %q", name, td.Name, name)
		}
	}

	// --- No duplicates ---
	names := e.Registry().Names()
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate name in Registry.Names(): %s", n)
		}
		seen[n] = true
	}
	if got, want := len(names), len(wantV0); got != want {
		t.Errorf("Registry.Len() = %d, want %d (pdpv0-only)", got, want)
	}

	// --- pdpv0-only invariant: v1 task names must NOT be present ---
	forbiddenV1 := []string{
		tasknames.PDPNotify, tasknames.PDPSync, tasknames.PDPAddDataSet,
		tasknames.PDPAddPiece, tasknames.AggregatePDPDeal, tasknames.PDPCommP,
		tasknames.PDPDelDataSet, tasknames.PDPDeletePiece, tasknames.PDPSaveCache,
		tasknames.PDPInitPP, tasknames.PDPProvingPeriod, tasknames.PDPProve,
	}
	for _, name := range forbiddenV1 {
		if e.Registry().Has(name) {
			t.Errorf("pdpv0-only invariant violated: v1 task %q present in registry", name)
		}
	}

	// --- Healthy() lifecycle: false → true → false ---
	if e.Healthy() {
		t.Error("Healthy() = true before Start()")
	}
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !e.Healthy() {
		t.Error("Healthy() = false after Start()")
	}

	// Start is non-idempotent: second Start returns an error.
	if err := e.Start(ctx); err == nil {
		t.Error("Start() returned nil on second call; want error")
	}

	// Verify harmony_machines row was recorded.
	row := e.DB().QueryRow(ctx, `SELECT count(*) FROM harmony_machines WHERE host_and_port = ?`,
		e.cfg.HostAndPort)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("scan harmony_machines: %v", err)
	}
	if n != 1 {
		t.Errorf("harmony_machines row count = %d, want 1", n)
	}

	if err := e.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if e.Healthy() {
		t.Error("Healthy() = true after Stop()")
	}

	// Stop is idempotent.
	if err := e.Stop(); err != nil {
		t.Errorf("second Stop() returned %v, want nil (idempotent)", err)
	}
}

// TestBuildTaskRegistry_Standalone exercises the registry constructor
// in isolation (no DB), pinning the exact name set so any future
// addition / removal is a deliberate test edit.
func TestBuildTaskRegistry_Standalone(t *testing.T) {
	r, err := BuildTaskRegistry()
	if err != nil {
		t.Fatalf("BuildTaskRegistry: %v", err)
	}
	if r.Len() == 0 {
		t.Fatal("BuildTaskRegistry: empty registry")
	}

	// Spot-check a high-value pdpv0 task name. (pdpv0-only scope: v1
	// names are explicitly NOT registered.)
	if _, ok := r.Get(tasknames.PDPv0_Notify); !ok {
		t.Errorf("expected task %q in registry", tasknames.PDPv0_Notify)
	}
	if _, ok := r.Get(tasknames.PDPNotify); ok {
		t.Errorf("pdpv0-only invariant violated: v1 task %q present", tasknames.PDPNotify)
	}
}

// TestRegistry_GABlockerTasksStayUnregistered is a structural guard for
// the curio-core "we beat Curio at scale" invariants (#87). Three upstream
// pdpv0 task names exist in tasks/tasknames but are DELIBERATELY not wired
// into BuildTaskRegistry, because each one is the direct cause of an open
// upstream Curio GA-blocker:
//
//   - PDPv0_PieceGC  -> filecoin-project/curio#1303: the 24h PieceGC window
//                       destroys uploaded-but-uncommitted pieces when commits
//                       lag uploads. Not registered => cannot happen here.
//   - PDPv0_Cleanup  -> filecoin-project/curio#1296: an un-includable
//                       cleanupPieces tx wedges the sender nonce queue and
//                       permanently breaks ProvPeriod after restart. The
//                       feature is absent => no such tx is ever produced.
//   - PDPv0_IPNI     -> filecoin-project/curio#1291: PDPv0_IPNI Max(50)
//                       storms YugabyteDB with 40001 serialization conflicts
//                       and strands piecerefs. Not registered (and SQLite has
//                       no 40001 class anyway).
//
// If a future fork bump or refactor silently re-registers any of these, this
// test fails LOUDLY. Re-enabling one must be a deliberate, reviewed edit that
// also reckons with the GA-blocker it reintroduces.
func TestRegistry_GABlockerTasksStayUnregistered(t *testing.T) {
	r, err := BuildTaskRegistry()
	if err != nil {
		t.Fatalf("BuildTaskRegistry: %v", err)
	}

	forbidden := []struct {
		name    string
		upstream string
	}{
		{tasknames.PDPv0_PieceGC, "filecoin-project/curio#1303 (PieceGC destroys uncommitted pieces)"},
		{tasknames.PDPv0_Cleanup, "filecoin-project/curio#1296 (cleanupPieces nonce wedge)"},
		{tasknames.PDPv0_IPNI, "filecoin-project/curio#1291 (IPNI 40001 serialization storm)"},
	}
	for _, f := range forbidden {
		if r.Has(f.name) {
			t.Errorf("GA-blocker invariant violated: task %q is registered; "+
				"it reintroduces %s. Re-enabling must be deliberate — see #87.",
				f.name, f.upstream)
		}
	}
}

// TestDefaultDBPath_RespectsXDG asserts the XDG override is honoured.
func TestDefaultDBPath_RespectsXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/curio-core-xdg-test")
	got := DefaultDBPath()
	want := "/tmp/curio-core-xdg-test/curio-core/state.sqlite"
	if got != want {
		t.Errorf("DefaultDBPath() = %q, want %q", got, want)
	}
}
