//go:build !linux && !darwin

package datapath

// listMounts on unsupported platforms returns just the root; labelled
// discovery still works under root, env/flag overrides still apply.
func listMounts() []string { return []string{"/"} }

// freeBytes is unavailable here; return 0 so discovery falls back to
// path-length/lexical tie-breaking instead of free-space ranking.
func freeBytes(string) uint64 { return 0 }
