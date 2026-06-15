//go:build !linux && !darwin

package gosigar

// Get on unsupported platforms returns ErrNotImplemented; callers
// (lotus/system) treat a zero Total as "unknown limit" and degrade safely.
func (m *Mem) Get() error  { return notImpl() }
func (s *Swap) Get() error { return notImpl() }
