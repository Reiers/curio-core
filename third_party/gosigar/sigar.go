// Package gosigar is a minimal, CGO-free drop-in replacement for
// github.com/elastic/gosigar, wired in via a go.mod replace directive.
//
// Why this exists (curio-core#80):
// Upstream gosigar's darwin implementation (sigar_darwin.go) requires CGO
// (sysctlbyname, Mach host_statistics). That makes a pure-Go darwin build
// of curio-core impossible: CGO=0 fails to compile gosigar, and CGO=1
// pulls in filecoin-ffi (the banned FFI path). The ONLY consumer of
// gosigar in curio-core's build graph is lotus/system, which calls
// `gosigar.Mem{}.Get()` and reads `.Total` to size memory budgets.
//
// This package provides the full public API surface upstream exposes (so
// any importer still compiles), but only Mem.Get() / Swap.Get() carry a
// real CGO-free implementation (see mem_*.go). Everything else returns
// ErrNotImplemented. That is sufficient because nothing in curio-core
// calls the process/cpu/filesystem APIs at runtime.
//
// Derived from elastic/gosigar (Apache-2.0); see LICENSE. Struct field
// layouts are kept byte-for-byte compatible with upstream v0.14.3.
package gosigar

import (
	"fmt"
	"time"
)

// ErrNotImplemented is returned by API surface this minimal replacement
// does not implement (everything except memory).
type ErrNotImplemented struct {
	OS string
}

func (e ErrNotImplemented) Error() string {
	return fmt.Sprintf("not implemented on %s by curio-core minimal gosigar", e.OS)
}

// IsNotImplemented reports whether err is an ErrNotImplemented.
func IsNotImplemented(err error) bool {
	switch err.(type) {
	case ErrNotImplemented, *ErrNotImplemented:
		return true
	default:
		return false
	}
}

// Sigar is the upstream collection interface, satisfied by ConcreteSigar.
type Sigar interface {
	CollectCpuStats(collectionInterval time.Duration) (<-chan Cpu, chan<- struct{})
	GetLoadAverage() (LoadAverage, error)
	GetMem() (Mem, error)
	GetSwap() (Swap, error)
	GetHugeTLBPages() (HugeTLBPages, error)
	GetFileSystemUsage(string) (FileSystemUsage, error)
	GetFDUsage() (FDUsage, error)
	GetRusage(who int) (Rusage, error)
}

type Cpu struct {
	User    uint64
	Nice    uint64
	Sys     uint64
	Idle    uint64
	Wait    uint64
	Irq     uint64
	SoftIrq uint64
	Stolen  uint64
}

func (cpu *Cpu) Total() uint64 {
	return cpu.User + cpu.Nice + cpu.Sys + cpu.Idle +
		cpu.Wait + cpu.Irq + cpu.SoftIrq + cpu.Stolen
}

func (cpu Cpu) Delta(other Cpu) Cpu {
	return Cpu{
		User:    cpu.User - other.User,
		Nice:    cpu.Nice - other.Nice,
		Sys:     cpu.Sys - other.Sys,
		Idle:    cpu.Idle - other.Idle,
		Wait:    cpu.Wait - other.Wait,
		Irq:     cpu.Irq - other.Irq,
		SoftIrq: cpu.SoftIrq - other.SoftIrq,
		Stolen:  cpu.Stolen - other.Stolen,
	}
}

type LoadAverage struct {
	One, Five, Fifteen float64
}

type Uptime struct {
	Length float64
}

type Mem struct {
	Total      uint64
	Used       uint64
	Free       uint64
	Cached     uint64
	ActualFree uint64
	ActualUsed uint64
}

type Swap struct {
	Total uint64
	Used  uint64
	Free  uint64
}

type HugeTLBPages struct {
	Total              uint64
	Free               uint64
	Reserved           uint64
	Surplus            uint64
	DefaultSize        uint64
	TotalAllocatedSize uint64
}

type CpuList struct {
	List []Cpu
}

type FDUsage struct {
	Open   uint64
	Unused uint64
	Max    uint64
}

type FileSystem struct {
	DirName     string
	DevName     string
	TypeName    string
	SysTypeName string
	Options     string
	Flags       uint32
}

type FileSystemList struct {
	List []FileSystem
}

type FileSystemUsage struct {
	Total     uint64
	Used      uint64
	Free      uint64
	Avail     uint64
	Files     uint64
	FreeFiles uint64
}

func (u *FileSystemUsage) UsePercent() float64 {
	b := u.Used + u.Avail
	if b == 0 {
		return 0
	}
	used := float64(u.Used)
	return used / float64(b) * 100.0
}

type ProcList struct {
	List []int
}

type RunState byte

type ProcState struct {
	Name      string
	Username  string
	State     RunState
	Ppid      int
	Pgid      int
	Tty       int
	Priority  int
	Nice      int
	Processor int
}

type ProcMem struct {
	Size        uint64
	Resident    uint64
	Share       uint64
	MinorFaults uint64
	MajorFaults uint64
	PageFaults  uint64
}

type ProcTime struct {
	StartTime uint64
	User      uint64
	Sys       uint64
	Total     uint64
}

type ProcArgs struct {
	List []string
}

type ProcEnv struct {
	Vars map[string]string
}

type ProcExe struct {
	Name string
	Cwd  string
	Root string
}

type ProcFDUsage struct {
	Open      uint64
	SoftLimit uint64
	HardLimit uint64
}

type Rusage struct {
	Utime    time.Duration
	Stime    time.Duration
	Maxrss   int64
	Ixrss    int64
	Idrss    int64
	Isrss    int64
	Minflt   int64
	Majflt   int64
	Nswap    int64
	Inblock  int64
	Oublock  int64
	Msgsnd   int64
	Msgrcv   int64
	Nsignals int64
	Nvcsw    int64
	Nivcsw   int64
}
