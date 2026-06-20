//go:build linux

package datapath

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

// listMounts reads /proc/mounts and returns real, writable mountpoints
// worth scanning for a labelled data dir. Pseudo/virtual filesystems
// (proc, sysfs, cgroup, tmpfs on system paths, etc.) are skipped.
func listMounts() []string {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return []string{"/"}
	}
	defer func() { _ = f.Close() }()

	skipFS := map[string]bool{
		"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
		"devtmpfs": true, "devpts": true, "mqueue": true, "hugetlbfs": true,
		"debugfs": true, "tracefs": true, "securityfs": true, "pstore": true,
		"bpf": true, "configfs": true, "fusectl": true, "binfmt_misc": true,
		"autofs": true, "rpc_pipefs": true, "nsfs": true, "overlay": true,
		"squashfs": true,
	}

	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		mountpoint, fstype := unescapeMount(fields[1]), fields[2]
		if skipFS[fstype] {
			continue
		}
		// skip obvious system mountpoints that won't hold operator data
		if mountpoint == "/boot" || strings.HasPrefix(mountpoint, "/boot/") ||
			strings.HasPrefix(mountpoint, "/proc") || strings.HasPrefix(mountpoint, "/sys") ||
			strings.HasPrefix(mountpoint, "/dev") || strings.HasPrefix(mountpoint, "/run") {
			continue
		}
		if seen[mountpoint] {
			continue
		}
		seen[mountpoint] = true
		out = append(out, mountpoint)
	}
	if len(out) == 0 {
		return []string{"/"}
	}
	return out
}

// unescapeMount decodes the octal escapes /proc/mounts uses for spaces
// etc. (e.g. "\040" -> " ").
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

// freeBytes returns bytes free on the filesystem containing path.
func freeBytes(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return uint64(st.Bavail) * uint64(st.Bsize)
}
