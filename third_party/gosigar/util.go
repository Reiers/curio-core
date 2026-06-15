package gosigar

import (
	"encoding/binary"
	"fmt"
	"unsafe"
)

// hostUint64 decodes 8 bytes using the host's native byte order. macOS on
// both arm64 and amd64 is little-endian, but decode via the detected order
// so this stays correct everywhere.
func hostUint64(b []byte) uint64 {
	if len(b) < 8 {
		return 0
	}
	return nativeEndian().Uint64(b)
}

func nativeEndian() binary.ByteOrder {
	var i uint16 = 1
	if (*[2]byte)(unsafe.Pointer(&i))[0] == 1 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

// FormatSize renders a byte count like upstream gosigar (e.g. "1.4G").
func FormatSize(size uint64) string {
	const (
		_ = iota
		K = 1 << (10 * iota)
		M
		G
		T
		P
		E
	)
	switch {
	case size >= E:
		return fmt.Sprintf("%.1fE", float64(size)/E)
	case size >= P:
		return fmt.Sprintf("%.1fP", float64(size)/P)
	case size >= T:
		return fmt.Sprintf("%.1fT", float64(size)/T)
	case size >= G:
		return fmt.Sprintf("%.1fG", float64(size)/G)
	case size >= M:
		return fmt.Sprintf("%.1fM", float64(size)/M)
	case size >= K:
		return fmt.Sprintf("%.1fK", float64(size)/K)
	default:
		return fmt.Sprintf("%dB", size)
	}
}

// FormatPercent renders a percentage like upstream.
func FormatPercent(percent float64) string {
	return fmt.Sprintf("%.1f%%", percent)
}
