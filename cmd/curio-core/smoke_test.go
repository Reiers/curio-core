// CLI error-scenario smoke matrix. Builds the curio-core binary once,
// then drives it with a table of bad inputs to assert we emit sensible
// errors rather than panicking, hanging, or silently mis-behaving.
//
// Inspired by filecoin-project/filecoin-pin#470.
//
// Run: go test ./cmd/curio-core -run TestSmoke -v
//
// Skipped under -short to keep the unit-test loop fast.

package main_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping smoke matrix in -short")
	}

	bin := buildBinary(t)

	tests := []struct {
		name           string
		args           []string
		wantExitNon0   bool
		wantStderrSubs []string // any one of these substrings is enough
		timeout        time.Duration
	}{
		{
			name:           "no_args",
			args:           []string{},
			wantExitNon0:   true,
			wantStderrSubs: []string{"PDP-only Curio", "Subcommands:"},
			timeout:        5 * time.Second,
		},
		{
			name:           "unknown_command",
			args:           []string{"fake-subcommand"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"unknown command", "fake-subcommand"},
			timeout:        5 * time.Second,
		},
		{
			name:           "help",
			args:           []string{"help"},
			wantExitNon0:   false,
			wantStderrSubs: []string{"PDP-only Curio"},
			timeout:        5 * time.Second,
		},
		{
			name:           "version",
			args:           []string{"version"},
			wantExitNon0:   false,
			wantStderrSubs: nil,
			timeout:        5 * time.Second,
		},
		{
			name:           "probe_bad_network",
			args:           []string{"probe", "--network", "definitely-not-a-network", "--data-dir", t.TempDir()},
			wantExitNon0:   true,
			wantStderrSubs: []string{"invalid", "network"},
			timeout:        10 * time.Second,
		},
		{
			// Unreachable PRIMARY gateway should NOT fail the probe — Lantern
			// falls back to its built-in Glif source (this is the resilient-
			// bootstrap behaviour from lantern#25 / V1.3). The expected
			// output is the standard success log, anchored against Glif.
			name:           "probe_unreachable_gateway_glif_fallback",
			args:           []string{"probe", "--network", "calibration", "--gateway", "http://127.0.0.1:1/rpc/v1", "--data-dir", t.TempDir(), "--timeout", "30s"},
			wantExitNon0:   false,
			wantStderrSubs: []string{"Anchored", "Stopped cleanly"},
			timeout:        45 * time.Second,
		},
		{
			name:           "run_bad_network",
			args:           []string{"run", "--network", "no-such-network", "--data-dir", t.TempDir(), "--no-lantern", "--listen", "127.0.0.1:0"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"invalid", "network"},
			timeout:        5 * time.Second,
		},
		{
			name:           "run_unwritable_data_dir",
			args:           []string{"run", "--network", "calibration", "--data-dir", "/proc/cannot-write-here", "--no-lantern", "--listen", "127.0.0.1:0"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"mkdir", "permission", "no such file"},
			timeout:        5 * time.Second,
		},
		{
			name:           "run_bad_listen_addr",
			args:           []string{"run", "--network", "calibration", "--data-dir", t.TempDir(), "--no-lantern", "--listen", "not-a-real-addr"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"address", "listen", "missing port"},
			timeout:        10 * time.Second,
		},
		{
			name:           "wallet_no_subcommand",
			args:           []string{"wallet"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"subcommand", "Subcommands:"},
			timeout:        5 * time.Second,
		},
		{
			name:           "wallet_help",
			args:           []string{"wallet", "help"},
			wantExitNon0:   false,
			wantStderrSubs: []string{"wallet management", "Subcommands:"},
			timeout:        5 * time.Second,
		},
		{
			name:           "wallet_list_missing_db",
			args:           []string{"wallet", "list", "--data-dir", "/no/such/path"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"state.sqlite", "not found"},
			timeout:        5 * time.Second,
		},
		{
			name:           "wallet_export_no_confirm",
			args:           []string{"wallet", "export", "0x6b4758baAcE34519F4977A30f6bEcd473249833c"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"--confirm", "plaintext"},
			timeout:        5 * time.Second,
		},
		{
			name:           "wallet_import_no_key",
			args:           []string{"wallet", "import"},
			wantExitNon0:   true,
			wantStderrSubs: []string{"private key", "--key"},
			timeout:        5 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tc.args...)
			stdout := &capBuf{}
			stderr := &capBuf{}
			cmd.Stdout = stdout
			cmd.Stderr = stderr
			cmd.Env = append(os.Environ(), "LANTERN_PASS=")

			err := cmd.Run()
			exitNon0 := err != nil
			if exitNon0 != tc.wantExitNon0 {
				t.Errorf("exit-non-zero = %v (err=%v); want %v\nstdout:\n%s\nstderr:\n%s",
					exitNon0, err, tc.wantExitNon0, stdout.String(), stderr.String())
			}

			if len(tc.wantStderrSubs) > 0 {
				combined := stderr.String() + stdout.String()
				found := false
				for _, sub := range tc.wantStderrSubs {
					if strings.Contains(strings.ToLower(combined), strings.ToLower(sub)) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("output missing any of %v\nstderr:\n%s\nstdout:\n%s",
						tc.wantStderrSubs, stderr.String(), stdout.String())
				}
			}
		})
	}
}

// buildBinary compiles curio-core into a tmp dir once per test run and
// returns the path. Subsequent t.Run cases reuse it.
//
// If CURIO_CORE_SMOKE_BIN is set, that path is used verbatim (so CI
// can build once with the right flags + reuse). If go is not on PATH
// (e.g. running a stripped test binary on Hetzner), the test skips.
func buildBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv("CURIO_CORE_SMOKE_BIN"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
		t.Fatalf("CURIO_CORE_SMOKE_BIN=%s does not exist", bin)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH and CURIO_CORE_SMOKE_BIN not set; skipping smoke matrix")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "curio-core")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := build.Run(); err != nil {
		t.Fatalf("build curio-core for smoke test: %v", err)
	}
	return bin
}

// capBuf is a small thread-safe sink for cmd output. exec.Cmd writes
// from a goroutine so we want simple sync.
type capBuf struct {
	mu  sync.Mutex
	buf []byte
}

func (c *capBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	return len(p), nil
}

func (c *capBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.buf)
}

// _ = fmt + io referenced indirectly via callers; keep imports stable.
var _ = fmt.Sprint
var _ io.Writer = (*capBuf)(nil)
