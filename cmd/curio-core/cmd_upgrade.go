// cmd_upgrade.go — opt-in self-update check (curio-core#68).
//
// Checks the GitHub Releases API for a newer tag than the running
// binary. By default it only REPORTS — it never replaces a running SP's
// binary silently. How the operator upgrades depends on how they
// installed:
//
//   - apt/dpkg install  -> `apt upgrade curio-core`
//   - rpm/dnf install   -> `dnf upgrade curio-core`
//   - macOS .pkg / brew -> `brew upgrade curio-core` (or reinstall .pkg)
//   - standalone binary -> `curio-core upgrade --apply` downloads the new
//     release asset for this os/arch and atomically replaces the binary
//     in place (requires write permission on the binary path; the daemon
//     must be restarted by the operator afterwards).
//
// Auto-upgrade is intentionally NOT a background loop in pre-alpha: a
// storage provider must never have its binary swapped mid-proof. The
// dashboard surfaces an "update available" banner (wired separately);
// this command is the CLI side of the same check.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const ghReleasesAPI = "https://api.github.com/repos/Reiers/curio-core/releases/latest"

type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func cmdUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "download the new release and replace this binary in place (standalone installs only)")
	includePre := fs.Bool("pre", false, "consider pre-release tags too")
	timeout := fs.Duration("timeout", 20*time.Second, "HTTP timeout for the GitHub API + download")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rel, err := fetchLatestRelease(ctx, *includePre)
	if err != nil {
		return err
	}

	current := versionTag
	latest := rel.TagName
	fmt.Printf("current: %s\nlatest:  %s\n", current, latest)

	cmp := compareVersions(current, latest)
	if cmp >= 0 {
		fmt.Println("curio-core is up to date.")
		return nil
	}

	fmt.Printf("\nA newer version is available: %s\n  %s\n\n", latest, rel.HTMLURL)

	if !*apply {
		fmt.Print(upgradeInstructions())
		return nil
	}

	return applyUpgrade(ctx, rel)
}

// fetchLatestRelease queries the GitHub releases API. With includePre we
// fall back to the most recent (possibly pre-release) tag from the list
// endpoint, since /releases/latest skips pre-releases.
func fetchLatestRelease(ctx context.Context, includePre bool) (*ghRelease, error) {
	url := ghReleasesAPI
	if includePre {
		url = "https://api.github.com/repos/Reiers/curio-core/releases?per_page=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "curio-core-upgrade")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query GitHub releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases published yet for Reiers/curio-core")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if includePre {
		var rels []ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&rels); err != nil {
			return nil, fmt.Errorf("decode releases: %w", err)
		}
		if len(rels) == 0 {
			return nil, fmt.Errorf("no releases published yet")
		}
		return &rels[0], nil
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &rel, nil
}

func upgradeInstructions() string {
	var b strings.Builder
	b.WriteString("To upgrade, use the method matching how you installed:\n\n")
	switch runtime.GOOS {
	case "linux":
		b.WriteString("  Debian/Ubuntu (.deb):  sudo apt update && sudo apt install --only-upgrade curio-core\n")
		b.WriteString("  RHEL/Fedora (.rpm):    sudo dnf upgrade curio-core\n")
		b.WriteString("  Standalone binary:     curio-core upgrade --apply\n")
	case "darwin":
		b.WriteString("  Homebrew:              brew upgrade curio-core\n")
		b.WriteString("  .pkg installer:        download + run the latest .pkg from the release page\n")
		b.WriteString("  Standalone binary:     curio-core upgrade --apply\n")
	default:
		b.WriteString("  Download the latest release for your platform from the release page.\n")
		b.WriteString("  Standalone binary:     curio-core upgrade --apply\n")
	}
	b.WriteString("\nAfter upgrading, restart the daemon. A running SP is never upgraded automatically.\n")
	return b.String()
}

// applyUpgrade downloads the release asset for this os/arch and atomically
// replaces the running executable. Standalone-install path only.
func applyUpgrade(ctx context.Context, rel *ghRelease) error {
	want := fmt.Sprintf("curio-core-%s-%s", runtime.GOOS, runtime.GOARCH)
	var assetURL string
	var assetSize int64
	for _, a := range rel.Assets {
		if a.Name == want {
			assetURL = a.BrowserDownloadURL
			assetSize = a.Size
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("release %s has no asset named %q (available: %s)", rel.TagName, want, assetNames(rel))
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return fmt.Errorf("resolve own binary: %w", err)
	}

	// Refuse if we can't write the target (package-managed install).
	dir := filepath.Dir(self)
	tmp, err := os.CreateTemp(dir, ".curio-core-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s (package-managed install? use the package manager): %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	fmt.Printf("downloading %s (%s)...\n", want, humanBytes(assetSize))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	req.Header.Set("User-Agent", "curio-core-upgrade")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}
	n, err := io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		return fmt.Errorf("write download: %w", err)
	}
	if assetSize > 0 && n != assetSize {
		return fmt.Errorf("short download: got %d of %d bytes", n, assetSize)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}

	// Atomic replace (same dir => same filesystem => rename is atomic).
	if err := os.Rename(tmpName, self); err != nil {
		return fmt.Errorf("replace binary at %s: %w", self, err)
	}
	fmt.Printf("\nupgraded %s -> %s\nrestart the daemon to run the new version.\n", versionTag, rel.TagName)
	return nil
}

func assetNames(rel *ghRelease) string {
	names := make([]string, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		names = append(names, a.Name)
	}
	return strings.Join(names, ", ")
}

func humanBytes(n int64) string {
	if n <= 0 {
		return "unknown size"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// compareVersions returns -1 if a<b, 0 if equal, +1 if a>b, using a
// lenient semver-ish comparison. Tags like "v1.2.3", "1.2.3",
// "0.0.1-prealpha" are handled: the leading "v" is stripped, a "-suffix"
// pre-release marker is split off, and dotted numeric fields compare
// left to right. Non-numeric or unparseable inputs compare as strings as
// a last resort (so we never crash, only mis-rank exotic tags).
func compareVersions(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	if r := compareNumericFields(an, bn); r != 0 {
		return r
	}
	// Equal core version: a pre-release (has suffix) is LOWER than the
	// same core without one (1.0.0-rc < 1.0.0). Two pre-releases compare
	// by suffix string.
	switch {
	case apre == "" && bpre == "":
		return 0
	case apre == "" && bpre != "":
		return 1
	case apre != "" && bpre == "":
		return -1
	default:
		return strings.Compare(apre, bpre)
	}
}

func splitVersion(v string) (nums []int, pre string) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	for _, f := range strings.Split(v, ".") {
		n, err := strconv.Atoi(f)
		if err != nil {
			// Unparseable field: treat the whole thing as 0 and let the
			// pre-release/string fallback decide.
			nums = append(nums, 0)
			continue
		}
		nums = append(nums, n)
	}
	return nums, pre
}

func compareNumericFields(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
