package gosigar

import (
	"runtime"
	"testing"
)

// TestMemGet is the regression guard for curio-core#80: lotus/system reads
// Mem.Total to size memory budgets, so on the platforms curio-core targets
// (linux, darwin) Get() must return a plausible non-zero total.
func TestMemGet(t *testing.T) {
	var m Mem
	err := m.Get()
	switch runtime.GOOS {
	case "linux", "darwin":
		if err != nil {
			t.Fatalf("Mem.Get() on %s returned error: %v", runtime.GOOS, err)
		}
		if m.Total == 0 {
			t.Fatalf("Mem.Get() on %s returned Total=0", runtime.GOOS)
		}
		// Sanity: between 256 MiB and 8 TiB.
		const min, max = 256 << 20, 8 << 40
		if m.Total < min || m.Total > max {
			t.Fatalf("Mem.Total=%d looks implausible (%s)", m.Total, FormatSize(m.Total))
		}
		t.Logf("%s total memory: %s (%d bytes)", runtime.GOOS, FormatSize(m.Total), m.Total)
	default:
		if !IsNotImplemented(err) {
			t.Fatalf("expected ErrNotImplemented on %s, got %v", runtime.GOOS, err)
		}
	}
}

func TestSwapGet(t *testing.T) {
	var s Swap
	if err := s.Get(); err != nil && !IsNotImplemented(err) {
		// Swap totals can legitimately be 0 (no swap); only a hard error fails.
		t.Fatalf("Swap.Get() error: %v", err)
	}
}

func TestNotImplemented(t *testing.T) {
	var pl ProcList
	if err := pl.Get(); !IsNotImplemented(err) {
		t.Fatalf("ProcList.Get() should be ErrNotImplemented, got %v", err)
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[uint64]string{
		512:                     "512B",
		2 * 1024:                "2.0K",
		3 * 1024 * 1024:         "3.0M",
		16 * 1024 * 1024 * 1024: "16.0G",
	}
	for in, want := range cases {
		if got := FormatSize(in); got != want {
			t.Errorf("FormatSize(%d)=%q want %q", in, got, want)
		}
	}
}
