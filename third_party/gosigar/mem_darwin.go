//go:build darwin

package gosigar

import "golang.org/x/sys/unix"

// Get reads total physical memory on darwin via sysctl hw.memsize.
// CGO-free: x/sys/unix.SysctlUint64 issues the sysctl(2) syscall directly.
// Free/used/cached are not populated (not needed by curio-core), but Total
// is the field lotus/system reads to size memory budgets.
func (m *Mem) Get() error {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return err
	}
	m.Total = total
	// We don't compute live free/used without Mach VM stats (CGO). Leave
	// them zero; lotus/system only consumes Total. ActualFree mirrors Free.
	return nil
}

// Get reads swap totals on darwin via sysctl vm.swapusage.
func (s *Swap) Get() error {
	xsw, err := unix.SysctlRaw("vm.swapusage")
	if err != nil {
		return err
	}
	// struct xsw_usage { uint64 xsu_total; uint64 xsu_avail; uint64 xsu_used; ... }
	if len(xsw) >= 24 {
		s.Total = hostUint64(xsw[0:8])
		s.Free = hostUint64(xsw[8:16])
		s.Used = hostUint64(xsw[16:24])
	}
	return nil
}
