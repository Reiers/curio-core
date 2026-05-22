package setupweb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func newTestDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{
		Path:        ":memory:",
		ForeignKeys: true,
	})
	if err != nil {
		t.Fatalf("harmonysqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestRedirect_NonSetupPathOnFreshDB asserts that hitting any non-setup
// path on an unconfigured DB issues a 303 to /setup.
func TestRedirect_NonSetupPathOnFreshDB(t *testing.T) {
	h := New(newTestDB(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// httptest.Client follows redirects by default; disable so we
	// can observe the 303.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSeeOther)
	}
	if got := resp.Header.Get("Location"); got != "/setup" {
		t.Errorf("Location = %q, want /setup", got)
	}
}

// TestRenderSetup_OnFreshDB asserts GET /setup returns 200 + HTML
// with all three input fields present.
func TestRenderSetup_OnFreshDB(t *testing.T) {
	h := New(newTestDB(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/setup")
	if err != nil {
		t.Fatalf("GET /setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, field := range []string{`name="market_address"`, `name="wallet_address"`, `name="miner_id"`} {
		if !strings.Contains(string(body), field) {
			t.Errorf("setup body missing %q\nbody=%s", field, body)
		}
	}
}

// TestSubmitSetup_HappyPath asserts a complete POST writes the
// default layer and redirects to /, then subsequent requests fall
// through to Inner.
func TestSubmitSetup_HappyPath(t *testing.T) {
	db := newTestDB(t)
	h := New(db)
	innerHit := false
	h.Inner = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerHit = true
		w.WriteHeader(http.StatusTeapot)
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	form := url.Values{}
	form.Set("market_address", "0xMARKET")
	form.Set("wallet_address", "0xWALLET")
	form.Set("miner_id", "f01234")
	resp, err := client.PostForm(srv.URL+"/api/setup", form)
	if err != nil {
		t.Fatalf("POST /api/setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 303 (body: %s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/" {
		t.Errorf("Location = %q, want /", got)
	}

	// Verify the bundle landed.
	st, err := config.Status(context.Background(), db)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.NeedsSetup {
		t.Errorf("post-submit NeedsSetup = true (missing=%v)", st.Missing)
	}

	// Now requests to / fall through to Inner.
	resp2, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / post-setup: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusTeapot {
		t.Errorf("post-setup GET / status = %d, want 418 (inner)", resp2.StatusCode)
	}
	if !innerHit {
		t.Error("Inner handler was not invoked after setup")
	}
}

// TestSubmitSetup_RejectsEmptyField asserts that POST with an empty
// field re-renders /setup with the error visible and pre-fills the
// fields that were supplied.
func TestSubmitSetup_RejectsEmptyField(t *testing.T) {
	h := New(newTestDB(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	form := url.Values{}
	form.Set("market_address", "0xMARKET")
	form.Set("wallet_address", "") // missing
	form.Set("miner_id", "f01234")
	resp, err := http.PostForm(srv.URL+"/api/setup", form)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (re-render)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "wallet_address") {
		t.Errorf("re-render body should mention wallet_address; got:\n%s", body)
	}
	if !strings.Contains(string(body), "0xMARKET") {
		t.Errorf("re-render should pre-fill market_address; body:\n%s", body)
	}
}

// TestSetupPathsAlwaysReachable asserts /setup is reachable even when
// the DB says "all configured" (the operator can revisit to update).
func TestSetupPathsAlwaysReachable(t *testing.T) {
	db := newTestDB(t)
	if err := config.UpsertDefaultLayer(context.Background(), db, config.ConfigBundle{
		Pdp: config.PdpSection{
			MarketAddress: "0xM", WalletAddress: "0xW", MinerID: "f01",
		},
	}); err != nil {
		t.Fatalf("UpsertDefaultLayer: %v", err)
	}

	h := New(db)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/setup")
	if err != nil {
		t.Fatalf("GET /setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 even when configured", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "already configured") {
		t.Errorf("page should hint 'already configured'; body:\n%s", body)
	}
}

// TestMethodNotAllowed asserts wrong methods on setup paths return 405.
func TestMethodNotAllowed(t *testing.T) {
	h := New(newTestDB(t))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/setup", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST /setup: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /setup status = %d, want 405", resp.StatusCode)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp2, err := client.Get(srv.URL + "/api/setup")
	if err != nil {
		t.Fatalf("GET /api/setup: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/setup status = %d, want 405", resp2.StatusCode)
	}
}
