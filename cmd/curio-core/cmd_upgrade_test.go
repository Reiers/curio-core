package main

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.2.0", "v1.10.0", -1}, // numeric, not lexical
		{"v1.10.0", "v1.2.0", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0", "v1.0.0.1", -1},       // shorter core < longer when extra >0
		{"v1.0.0.0", "v1.0.0", 0},        // trailing zeros equal
		{"1.0.0-rc1", "1.0.0", -1},       // pre-release < release
		{"1.0.0", "1.0.0-rc1", 1},        // release > pre-release
		{"1.0.0-rc1", "1.0.0-rc2", -1},   // pre-release suffix string order
		{"0.0.1-prealpha", "v0.1.0", -1}, // real current vs first tag
		{"v0.1.0", "0.0.1-prealpha", 1},  // and the reverse
		{"v1.7.0", "v1.7.0", 0},
		{"v1.6.4", "v1.7.0", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:         "unknown size",
		512:       "512 B",
		1024:      "1.0 KiB",
		107954439: "103.0 MiB",
		75198626:  "71.7 MiB",
	}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
