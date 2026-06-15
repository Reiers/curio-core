//go:build linux

package gosigar

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Get reads memory totals from /proc/meminfo (CGO-free). Fields are in kB.
func (m *Mem) Get() error {
	mi, err := parseMeminfo()
	if err != nil {
		return err
	}
	m.Total = mi["MemTotal"]
	m.Free = mi["MemFree"]
	m.Cached = mi["Cached"]
	if avail, ok := mi["MemAvailable"]; ok {
		m.ActualFree = avail
	} else {
		m.ActualFree = m.Free + m.Cached + mi["Buffers"]
	}
	if m.Total >= m.ActualFree {
		m.ActualUsed = m.Total - m.ActualFree
	}
	if m.Total >= m.Free {
		m.Used = m.Total - m.Free
	}
	return nil
}

// Get reads swap totals from /proc/meminfo.
func (s *Swap) Get() error {
	mi, err := parseMeminfo()
	if err != nil {
		return err
	}
	s.Total = mi["SwapTotal"]
	s.Free = mi["SwapFree"]
	if s.Total >= s.Free {
		s.Used = s.Total - s.Free
	}
	return nil
}

func parseMeminfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]uint64, 48)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		// Values are in kB unless unitless; convert kB -> bytes.
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		out[key] = v
	}
	return out, sc.Err()
}
