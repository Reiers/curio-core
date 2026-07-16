package dashboard

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRenderPreview renders the dashboard pages with representative mock
// data to /tmp/curio-core-preview so the redesign can be screenshotted
// without a live daemon/DB. Gated behind PREVIEW=1 so it never runs in
// normal `go test`.
//
//	PREVIEW=1 go test ./internal/dashboard/ -run TestRenderPreview -count=1
func TestRenderPreview(t *testing.T) {
	if os.Getenv("PREVIEW") == "" {
		t.Skip("set PREVIEW=1 to emit HTML preview")
	}
	outDir := "/tmp/curio-core-preview"
	if err := os.MkdirAll(filepath.Join(outDir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Parse templates exactly like NewServer, but from disk.
	tmpl := template.New("").Funcs(funcMap())
	matches, err := filepath.Glob("templates/*.html")
	if err != nil || len(matches) == 0 {
		t.Fatalf("glob templates: %v (%d)", err, len(matches))
	}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(m), ".html")
		if _, err := tmpl.New(name).Parse(string(b)); err != nil {
			t.Fatalf("parse %s: %v", m, err)
		}
	}

	// Copy static assets so /static/* resolves when served from outDir.
	staticFiles, _ := filepath.Glob("static/*")
	for _, f := range staticFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(outDir, "static", filepath.Base(f)), b, 0o644)
	}

	build := BuildInfo{Version: "v0.1.0-beta.2", Network: "calibration"}
	cfg := Config{
		Network:      "calibration",
		Version:      "v0.1.0-beta.2",
		PayeeAddress: "0x1c9a4e2b7f6d3a08c1e5b9d2f4a7c60318be92d4",
		StashDir:     "/var/lib/curio-core/stash",
		DataDir:      "/var/lib/curio-core",
	}

	now := time.Now().UTC()
	price := filPrice{USD: 3.214, Fresh: true, AsOf: now}

	ov := overviewData{
		NowUTC: now.Format(time.RFC3339),
		Chain: overviewChain{
			HeadEpoch: 3881573, NetworkID: "calibration",
			RPCAddress: "lantern (embedded, in-process)",
			Reachable:  true, Synced: true, Version: build.Version,
			ChainID: 314159, Peers: 24, PendingTxCnt: 1,
		},
		Stats: overviewStats{
			DatasetsActive: 3, DatasetsTerminated: 1,
			PiecesCompleteCount: 128, PiecesCompleteBytes: 274877906944,
			RailsActive: 2, RailsTerminated: 0,
			RecentProveSuccess24: 47, RecentProveFailed24: 0,
			TasksRunningNow: 2, TasksUnowned: 5,
		},
		Price: price,
		Fin: finRollup{
			ActiveRails:  2,
			RatePerEpoch: "3472222222222222",      // ~0.00347 USDFC
			RatePerDay:   "10000000000000000000",  // 10 USDFC
			RatePer30d:   "300000000000000000000", // 300 USDFC
			Fresh:        true,
		},
	}
	// Exercise the real readiness path (eth nil => funded=unknown).
	s := &Server{cfg: cfg}
	ov.Readiness = s.computeReadiness(context.Background(), ov)

	wallets := walletsData{
		Price: price,
		Wallets: []walletRow{
			{Address: "0x1c9a4e2b7f6d3a08c1e5b9d2f4a7c60318be92d4", Role: "pdp", IsPDP: true, FILWei: "42.318", USDFC: "1204.55", FILUsd: usdMoney(42.318 * price.USD)},
			{Address: "0x7b3f9c1d5a2e8046b9f0c3d7e1a45268903fce17", Role: "sender", FILWei: "3.5", USDFC: "0", FILUsd: usdMoney(3.5 * price.USD)},
		},
	}

	render := func(name, title, active string, data any) {
		f, err := os.Create(filepath.Join(outDir, name+".html"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		pd := pageData{Title: title, Build: build, Cfg: cfg, Active: active, Data: data}
		if err := tmpl.ExecuteTemplate(f, name, pd); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
	}

	render("overview", "Overview", "overview", ov)
	render("wallets", "Wallets", "wallets", wallets)
	render("guide", "Setup Guide", "guide", s.computeGuide(context.Background(), ov))

	t.Logf("wrote preview to %s", outDir)
}
