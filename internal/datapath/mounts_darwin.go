//go:build darwin

package datapath

import (
	"syscall"
)

// listMounts returns mountpoints on darwin via getfsstat(2). Common
// system/read-only mounts are filtered so discovery focuses on real
// data volumes (typically "/" and anything under /Volumes).
func listMounts() []string {
	n, err := syscall.Getfsstat(nil, 1 /* MNT_NOWAIT */)
	if err != nil || n <= 0 {
		return []string{"/"}
	}
	bufs := make([]syscall.Statfs_t, n)
	if _, err := syscall.Getfsstat(bufs, 1); err != nil {
		return []string{"/"}
	}

	var out []string
	seen := map[string]bool{}
	for i := range bufs {
		mp := int8SliceToString(bufs[i].Mntonname[:])
		if mp == "" || seen[mp] {
			continue
		}
		// skip Apple system read-only/synthetic mounts
		switch {
		case mp == "/dev",
			mp == "/System/Volumes/VM",
			mp == "/System/Volumes/Preboot",
			mp == "/System/Volumes/Update",
			mp == "/System/Volumes/xarts",
			mp == "/System/Volumes/iSCPreboot",
			mp == "/System/Volumes/Hardware":
			continue
		}
		seen[mp] = true
		out = append(out, mp)
	}
	if len(out) == 0 {
		return []string{"/"}
	}
	return out
}

func int8SliceToString(b []int8) string {
	buf := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		buf = append(buf, byte(c))
	}
	return string(buf)
}

// freeBytes returns bytes free on the filesystem containing path.
func freeBytes(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}
