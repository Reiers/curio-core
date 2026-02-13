package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

func Run(homeDir, dataDir string) ([]Check, error) {
	checks := []Check{checkAria2c(), checkDisk(homeDir), checkWritable(dataDir)}
	return checks, nil
}

func RunPostImport(dataDir string) ([]Check, error) {
	checks := []Check{checkChainHead(dataDir), checkBlockstorePopulated(dataDir), checkStateRootReachable(dataDir)}
	return checks, nil
}

func checkAria2c() Check {
	if _, err := exec.LookPath("aria2c"); err != nil {
		return Check{
			Name:    "aria2c-installed",
			OK:      false,
			Message: "aria2c not found on PATH",
			Fix:     "Install aria2 (macOS: brew install aria2, Ubuntu: sudo apt install aria2)",
		}
	}
	return Check{Name: "aria2c-installed", OK: true, Message: "aria2c detected"}
}

func checkDisk(path string) Check {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return Check{Name: "disk-space", OK: false, Message: fmt.Sprintf("cannot prepare path %s: %v", path, err), Fix: "Ensure the directory exists and is accessible"}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return Check{Name: "disk-space", OK: false, Message: fmt.Sprintf("cannot read filesystem stats: %v", err), Fix: "Check filesystem permissions and mount state"}
	}
	available := stat.Bavail * uint64(stat.Bsize)
	const min = uint64(100 * 1024 * 1024 * 1024) // 100 GiB guidance
	if available < min {
		return Check{Name: "disk-space", OK: false, Message: fmt.Sprintf("low free space at %s: %.1f GiB", path, float64(available)/(1024*1024*1024)), Fix: "Free at least 100 GiB or use --data-dir on a larger disk"}
	}
	return Check{Name: "disk-space", OK: true, Message: fmt.Sprintf("available space at %s: %.1f GiB", path, float64(available)/(1024*1024*1024))}
}

func checkWritable(dataDir string) Check {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Check{Name: "data-dir-writable", OK: false, Message: fmt.Sprintf("cannot create data dir %s: %v", dataDir, err), Fix: "Choose a writable --data-dir or fix permissions"}
	}
	testPath := filepath.Join(dataDir, ".write-test")
	if err := os.WriteFile(testPath, []byte("ok"), 0o644); err != nil {
		return Check{Name: "data-dir-writable", OK: false, Message: fmt.Sprintf("cannot write in %s: %v", dataDir, err), Fix: "Run chmod/chown to grant write permission for current user"}
	}
	_ = os.Remove(testPath)
	return Check{Name: "data-dir-writable", OK: true, Message: fmt.Sprintf("data dir is writable: %s", dataDir)}
}

func checkChainHead(dataDir string) Check {
	headFile := filepath.Join(dataDir, "chainstore", "head.json")
	b, err := os.ReadFile(headFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Check{Name: "chain-head-exists", OK: false, Message: "chain head missing", Fix: "Re-run snapshot import and ensure head is committed"}
		}
		return Check{Name: "chain-head-exists", OK: false, Message: err.Error(), Fix: "Inspect chainstore/head.json permissions"}
	}
	if len(b) == 0 {
		return Check{Name: "chain-head-exists", OK: false, Message: "chain head file empty", Fix: "Re-import snapshot"}
	}
	return Check{Name: "chain-head-exists", OK: true, Message: "chain head exists"}
}

func checkBlockstorePopulated(dataDir string) Check {
	dir := filepath.Join(dataDir, "blockstore")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return Check{Name: "blockstore-populated", OK: false, Message: "blockstore missing", Fix: "Re-run snapshot import"}
	}
	if len(ents) == 0 {
		return Check{Name: "blockstore-populated", OK: false, Message: "blockstore empty", Fix: "Re-run snapshot import"}
	}
	return Check{Name: "blockstore-populated", OK: true, Message: fmt.Sprintf("blockstore contains %d blocks", len(ents))}
}

func checkStateRootReachable(dataDir string) Check {
	headFile := filepath.Join(dataDir, "chainstore", "head.json")
	b, err := os.ReadFile(headFile)
	if err != nil {
		return Check{Name: "state-root-reachable", OK: false, Message: "chain head unavailable", Fix: "Re-run snapshot import"}
	}
	var h struct {
		StateRoot string `json:"stateRoot"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return Check{Name: "state-root-reachable", OK: false, Message: "invalid head format", Fix: "Re-run snapshot import with latest binary"}
	}
	if h.StateRoot == "" {
		return Check{Name: "state-root-reachable", OK: false, Message: "state root missing", Fix: "Import a snapshot containing state root metadata"}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "blockstore", h.StateRoot+".blk")); err != nil {
		return Check{Name: "state-root-reachable", OK: false, Message: "state root block not found in blockstore", Fix: "Re-run import to rebuild blockstore"}
	}
	return Check{Name: "state-root-reachable", OK: true, Message: "state root reachable"}
}
