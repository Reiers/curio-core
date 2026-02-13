package snapshot

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
)

var pctRe = regexp.MustCompile(`\((\d+)%\)`)

func Download(log *logging.Logger, st *status.Store, network, url, outDir string, concurrency int) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	if err := st.Set("downloading", 1, "starting snapshot download"); err != nil {
		return "", err
	}

	outFile := filepath.Join(outDir, fmt.Sprintf("%s-latest.car.zst", network))
	args := []string{"-x", fmt.Sprintf("%d", concurrency), "--summary-interval=1", "--console-log-level=notice", "-d", outDir, "-o", filepath.Base(outFile), url}
	cmd := exec.Command("aria2c", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed starting aria2c: %w", err)
	}

	s := bufio.NewScanner(stdout)
	for s.Scan() {
		line := s.Text()
		if m := pctRe.FindStringSubmatch(line); len(m) == 2 {
			p := 0
			fmt.Sscanf(m[1], "%d", &p)
			_ = st.Set("downloading", p, strings.TrimSpace(line))
		}
	}
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("aria2c failed: %w", err)
	}
	_ = st.Set("downloading", 100, "download complete")
	return outFile, nil
}

func Verify(log *logging.Logger, st *status.Store, file string) error {
	_ = st.Set("verifying", 0, "verifying snapshot")
	fi, err := os.Stat(file)
	if err != nil {
		return err
	}
	if fi.Size() < 1024*1024 {
		return errors.New("snapshot too small; sanity check failed")
	}
	_ = st.Set("verifying", 100, fmt.Sprintf("verified snapshot (%d bytes)", fi.Size()))
	log.Infof("verified snapshot %s size=%d", file, fi.Size())
	return nil
}

func CleanupFile(log *logging.Logger, st *status.Store, file string) error {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = st.Set("cleanup", 100, "snapshot deleted")
	log.Infof("snapshot deleted %s", file)
	return nil
}

func CleanupDir(log *logging.Logger, st *status.Store, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	_ = st.Set("cleanup", 100, "snapshot directory cleaned")
	return nil
}
