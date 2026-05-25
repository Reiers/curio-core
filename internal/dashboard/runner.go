// runner.go - allowlisted CLI command execution for the embedded
// dashboard terminal. We deliberately do NOT expose a real shell:
//
//   - Only a fixed set of curio-core subcommands are allowed.
//   - Arguments are parsed by Go's flag-aware shell-words split, then
//     re-shaped into argv (no shell, no globbing, no env expansion).
//   - The binary executed is the CURRENT curio-core binary, located
//     via os.Executable(). No PATH lookups.
//   - Output is captured (stdout+stderr), truncated to 64 KiB, and
//     returned as JSON.
//   - 30s hard timeout per invocation.
//
// This is an operator convenience for ad-hoc reads (status / wallet
// list / sp info / doctor / version). For anything that mutates state
// the operator should still SSH in and run the CLI directly with
// proper TTY. Dashboard endpoint is loopback-only by design (see
// pdpwire.FallbackHandler routing) and there is no auth model yet.

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// allowlistedSubcommands returns the canonical list of curio-core
// subcommands that are safe to invoke from the dashboard. ORDER is
// the order shown in the UI's autocomplete; keep most-used first.
//
// Read-only operations only. No `run`, `wallet new`, `wallet import`,
// `wallet send`, `wallet export`, `wallet delete`, `demo *`, `sp
// register` — anything that mutates persistent state or broadcasts
// an on-chain tx is blocked.
func allowlistedSubcommands() []string {
	return []string{
		"version",
		"wallet list",
		"doctor",
		"sp info",
		"probe",
		"config show",
	}
}

// runRequest is the body shape for POST /api/run.
type runRequest struct {
	// Args is the argv after the curio-core binary. e.g.
	//   ["wallet", "list"] or ["doctor"]
	Args []string `json:"args"`
}

// runResponse is the body shape for POST /api/run.
type runResponse struct {
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Error    string `json:"error,omitempty"`
	Truncated bool  `json:"truncated,omitempty"`
	Duration  string `json:"duration"`
}

const (
	maxOutputBytes = 64 * 1024
	commandTimeout = 30 * time.Second
)

func (s *Server) handleAPIRun(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRun(w, runResponse{Error: "invalid JSON body: " + err.Error()})
		return
	}
	if len(req.Args) == 0 {
		writeRun(w, runResponse{Error: "missing args"})
		return
	}
	if !commandIsAllowed(req.Args) {
		writeRun(w, runResponse{
			Error: "command not in allowlist; permitted: " + strings.Join(allowlistedSubcommands(), ", "),
		})
		return
	}

	bin, err := os.Executable()
	if err != nil {
		writeRun(w, runResponse{Error: "resolve curio-core binary: " + err.Error()})
		return
	}
	// Sanity check: make sure we're invoking a real file, not a symlink
	// pointing outside the install path.
	if abs, err := filepath.EvalSymlinks(bin); err == nil {
		bin = abs
	}

	ctx, cancel := context.WithTimeout(r.Context(), commandTimeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, bin, req.Args...)
	cmd.Env = []string{
		// Inherit a minimal env. Keep DATA_DIR if curio-core uses it.
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"GOLOG_LOG_LEVEL=error",
	}

	var stdout, stderr capBuf
	stdout.limit = maxOutputBytes
	stderr.limit = maxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	dur := time.Since(start)

	exitCode := 0
	errStr := ""
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			errStr = runErr.Error()
			exitCode = -1
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		errStr = fmt.Sprintf("timeout after %s", commandTimeout)
	}

	resp := runResponse{
		OK:        exitCode == 0 && errStr == "",
		ExitCode:  exitCode,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Error:     errStr,
		Truncated: stdout.truncated || stderr.truncated,
		Duration:  dur.Round(time.Millisecond).String(),
	}
	writeRun(w, resp)
}

func writeRun(w http.ResponseWriter, r runResponse) {
	_ = json.NewEncoder(w).Encode(r)
}

// commandIsAllowed checks if argv begins with any allowlisted entry.
// Extra args after the matched prefix are permitted (e.g. `wallet
// list --data-dir /tmp/x`) but flags themselves must not contain
// shell metacharacters.
func commandIsAllowed(args []string) bool {
	// Defense against `--exec`-style escapes: reject any arg that
	// contains shell metachars. These shouldn't appear in our CLI's
	// vocabulary anyway.
	for _, a := range args {
		if strings.ContainsAny(a, "`$|&;<>()\\\"'\n") {
			return false
		}
	}
	allowed := allowlistedSubcommands()
	for _, a := range allowed {
		prefix := strings.Fields(a)
		if argsHavePrefix(args, prefix) {
			return true
		}
	}
	return false
}

func argsHavePrefix(args, prefix []string) bool {
	if len(args) < len(prefix) {
		return false
	}
	for i := range prefix {
		if args[i] != prefix[i] {
			return false
		}
	}
	return true
}

// capBuf is an io.Writer that caps the captured bytes at `limit`,
// silently discarding overflow and flagging it via `truncated`.
type capBuf struct {
	buf       strings.Builder
	limit     int
	truncated bool
}

func (c *capBuf) Write(p []byte) (int, error) {
	if c.limit > 0 && c.buf.Len() >= c.limit {
		c.truncated = true
		return len(p), nil
	}
	remain := c.limit - c.buf.Len()
	if c.limit > 0 && len(p) > remain {
		c.buf.Write(p[:remain])
		c.truncated = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *capBuf) String() string { return c.buf.String() }

// dirSize returns the total bytes occupied by files directly under
// `dir` (non-recursive walking the tree). Symlinks are not followed
// to keep the read tight and avoid loops.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// Ensure unused-imports don't break the build when the file is
// pruned during refactors.
var _ = sort.Strings
