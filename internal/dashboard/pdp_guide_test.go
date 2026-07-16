package dashboard

import (
	"context"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fresh SP: no wallet, no stash, chain offline. Everything should read as a
// failure/locked walkthrough with the first critical step surfaced as "next".
func TestComputeGuide_FreshNode(t *testing.T) {
	s := &Server{cfg: Config{}} // nothing configured, no eth client
	ov := overviewData{
		Chain: overviewChain{Reachable: false, Synced: false},
	}
	g := s.computeGuide(context.Background(), ov)

	if g.Total != 6 {
		t.Fatalf("want 6 canonical steps, got %d", g.Total)
	}
	if g.AllReady {
		t.Fatal("fresh node must not be AllReady")
	}
	if g.NextNum != 1 || g.Next == nil || g.Next.Key != "chain" {
		t.Fatalf("next step should be #1 chain, got num=%d next=%v", g.NextNum, g.Next)
	}
	// Steps are numbered 1..N in canonical order.
	for i, st := range g.Steps {
		if st.Num != i+1 {
			t.Errorf("step %d has Num %d", i, st.Num)
		}
	}
	// The "funded" step depends on a wallet that doesn't exist yet, so it
	// must be locked, never a false "action needed".
	funded := stepByKey(t, g, "funded")
	if !funded.Locked {
		t.Errorf("funded step should be locked before a wallet exists; state=%s locked=%v", funded.State, funded.Locked)
	}
	// A non-ok critical step that is NOT locked should carry remediation.
	chain := stepByKey(t, g, "chain")
	if chain.Locked {
		t.Fatal("chain step (first) must not be locked")
	}
	if len(chain.Actions) == 0 {
		t.Error("unsatisfied, unlocked chain step should offer at least one action")
	}
}

// fully configured + healthy: all critical steps ok, guide reports ready.
func TestComputeGuide_AllReady(t *testing.T) {
	s := &Server{cfg: Config{
		PayeeAddress: "0x1c9a4e2b7f6d3a08c1e5b9d2f4a7c60318be92d4",
		StashDir:     "/var/lib/curio-core/stash",
	}}
	// eth client stays nil => funded reads "unknown", which is NOT ok, so to
	// exercise AllReady we assert the critical set minus the balance read.
	ov := overviewData{
		Chain: overviewChain{Reachable: true, Synced: true},
		Stats: overviewStats{DatasetsActive: 2, RecentProveSuccess24: 10, RecentProveFailed24: 0},
	}
	g := s.computeGuide(context.Background(), ov)

	// chain / wallet / stash / datasets / proving should all be ok; funded is
	// unknown (no eth client) which keeps AllReady false but must NOT crash
	// and must be surfaced as the next actionable step.
	chain := stepByKey(t, g, "chain")
	if !chain.Done() {
		t.Errorf("chain should be done, state=%s", chain.State)
	}
	wallet := stepByKey(t, g, "wallet")
	if !wallet.Done() {
		t.Errorf("wallet should be done, state=%s", wallet.State)
	}
	datasets := stepByKey(t, g, "datasets")
	if !datasets.Done() {
		t.Errorf("datasets should be done, state=%s", datasets.State)
	}
	// funded is the only outstanding critical item => it is "next".
	if g.Next == nil || g.Next.Key != "funded" {
		t.Errorf("funded should be the next step when it's the only gap, got %v", g.Next)
	}
	if g.Next.Locked {
		t.Error("funded must be unlocked once a wallet exists")
	}
}

// The guide template must render against a real report without erroring.
func TestGuideTemplateRenders(t *testing.T) {
	tmpl := template.New("").Funcs(funcMap())
	for _, m := range mustGlob(t, "templates/*.html") {
		b, err := os.ReadFile(m)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(m), ".html")
		if _, err := tmpl.New(name).Parse(string(b)); err != nil {
			t.Fatalf("parse %s: %v", m, err)
		}
	}

	// chain up, but no wallet yet => the wallet step is the first unmet
	// critical step, so it renders its remediation command.
	s := &Server{cfg: Config{StashDir: "/var/lib/curio-core/stash"}}
	ov := overviewData{Chain: overviewChain{Reachable: true, Synced: true}}
	g := s.computeGuide(context.Background(), ov)

	var buf strings.Builder
	pd := pageData{Title: "Setup Guide", Build: BuildInfo{Version: "test", Network: "calibration"}, Cfg: s.cfg, Active: "guide", Data: g}
	if err := tmpl.ExecuteTemplate(&buf, "guide", pd); err != nil {
		t.Fatalf("render guide: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Connect the chain node", "Configure the PDP wallet", "Setup Guide", "curio-core wallet new"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered guide missing %q", want)
		}
	}
}

func stepByKey(t *testing.T, g guideReport, key string) guideStep {
	t.Helper()
	for _, st := range g.Steps {
		if st.Key == key {
			return st
		}
	}
	t.Fatalf("no guide step with key %q", key)
	return guideStep{}
}

func mustGlob(t *testing.T, pat string) []string {
	t.Helper()
	m, err := filepath.Glob(pat)
	if err != nil || len(m) == 0 {
		t.Fatalf("glob %s: %v (%d)", pat, err, len(m))
	}
	return m
}
