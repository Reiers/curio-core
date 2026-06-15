package gosigar

import (
	"runtime"
	"time"
)

// ConcreteSigar implements Sigar. Only memory is backed by a real
// (CGO-free) implementation; the rest return ErrNotImplemented.
type ConcreteSigar struct{}

func (c *ConcreteSigar) CollectCpuStats(collectionInterval time.Duration) (<-chan Cpu, chan<- struct{}) {
	samplesCh := make(chan Cpu, 1)
	stopCh := make(chan struct{})
	// CPU stats are not implemented in this minimal build; emit a single
	// zero sample and stop, so callers ranging over the channel don't hang.
	go func() {
		samplesCh <- Cpu{}
		<-stopCh
	}()
	return samplesCh, stopCh
}

func (c *ConcreteSigar) GetLoadAverage() (LoadAverage, error) {
	l := LoadAverage{}
	return l, l.Get()
}

func (c *ConcreteSigar) GetMem() (Mem, error) {
	m := Mem{}
	return m, m.Get()
}

func (c *ConcreteSigar) GetSwap() (Swap, error) {
	s := Swap{}
	return s, s.Get()
}

func (c *ConcreteSigar) GetHugeTLBPages() (HugeTLBPages, error) {
	p := HugeTLBPages{}
	return p, p.Get()
}

func (c *ConcreteSigar) GetFileSystemUsage(path string) (FileSystemUsage, error) {
	f := FileSystemUsage{}
	return f, f.Get(path)
}

func (c *ConcreteSigar) GetFDUsage() (FDUsage, error) {
	fd := FDUsage{}
	return fd, fd.Get()
}

func (c *ConcreteSigar) GetRusage(who int) (Rusage, error) {
	r := Rusage{}
	return r, r.Get(who)
}

func notImpl() error { return ErrNotImplemented{OS: runtime.GOOS} }

// Get methods. Mem and Swap are implemented per-OS in mem_*.go; the rest
// are stubs (nothing in curio-core's graph calls them).

func (l *LoadAverage) Get() error           { return notImpl() }
func (c *Cpu) Get() error                   { return notImpl() }
func (u *Uptime) Get() error                { return notImpl() }
func (p *HugeTLBPages) Get() error          { return notImpl() }
func (fd *FDUsage) Get() error              { return notImpl() }
func (fl *FileSystemList) Get() error       { return notImpl() }
func (f *FileSystemUsage) Get(string) error { return notImpl() }
func (pl *ProcList) Get() error             { return notImpl() }
func (ps *ProcState) Get(int) error         { return notImpl() }
func (pm *ProcMem) Get(int) error           { return notImpl() }
func (pt *ProcTime) Get(int) error          { return notImpl() }
func (pa *ProcArgs) Get(int) error          { return notImpl() }
func (r *Rusage) Get(int) error             { return notImpl() }
