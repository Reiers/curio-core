// Package setupweb serves the curio-core first-run setup flow.
//
// It exposes one HTTP router that:
//
//   - Renders GET /setup as a minimal HTML form (market_address,
//     wallet_address, miner_id).
//   - Accepts POST /api/setup with x-www-form-urlencoded body, validates
//     every required field, calls
//     config.UpsertDefaultLayer, and redirects to /.
//   - Wraps any inner handler with a middleware that, on every request,
//     re-checks firstrun.Status: if NeedsSetup is true and the path is
//     neither /setup nor /api/setup, redirect to /setup. Once the
//     default layer is complete, requests fall through to the inner
//     handler.
//
// This package is intentionally CGO-free and standalone so it can be
// mounted from `curio-core run` without dragging in web/srv.go's
// upstream Curio deps (lotus metrics, curio/deps, etc., which are
// gated behind //go:build cgo until the Day 6 carve-out).
//
// Authentication: the /setup endpoints bypass any upstream auth
// middleware. By definition there is no operator account configured
// yet — anyone on the loopback interface is the "first user" and
// completes the wizard.
package setupweb

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// Handler is the HTTP handler that owns the /setup + /api/setup
// routes and (optionally) wraps an inner handler with the
// first-run redirect middleware.
type Handler struct {
	db *harmonysqlite.DB

	// Inner is the handler served for every request that is NOT
	// /setup or /api/setup once first-run is complete. May be nil;
	// nil produces a small 404 placeholder.
	Inner http.Handler
}

// New constructs a Handler bound to the given state DB.
func New(db *harmonysqlite.DB) *Handler {
	return &Handler{db: db}
}

// ServeHTTP routes requests through the first-run middleware first,
// then dispatches to /setup or /api/setup, falling through to Inner
// for everything else.
//
// The router is deliberately tiny (path equality + method check) so
// it has zero gorilla/mux dependencies. Callers that need a richer
// route tree can mount this handler at "/" inside their own router
// and rely on it stepping aside for non-setup paths.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	st, err := config.Status(r.Context(), h.db)
	if err != nil {
		http.Error(w, "first-run probe failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Setup-flow endpoints always serve their own content.
	switch {
	case path == "/setup" && r.Method == http.MethodGet:
		h.renderSetup(w, r, st, nil, nil)
		return
	case path == "/api/setup" && r.Method == http.MethodPost:
		h.submitSetup(w, r, st)
		return
	case path == "/setup" || path == "/api/setup":
		// Wrong method for a setup path. Make it explicit.
		w.Header().Set("Allow", methodFor(path))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Not a setup-flow path. If first-run is incomplete, redirect.
	if st.NeedsSetup {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	// Otherwise, fall through.
	if h.Inner != nil {
		h.Inner.ServeHTTP(w, r)
		return
	}
	// Day 5 has no main UI yet; show a tiny placeholder so the
	// liveness check is at least informative.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintln(w, "curio-core: ready (no main UI mounted)")
}

func methodFor(path string) string {
	if path == "/setup" {
		return http.MethodGet
	}
	return http.MethodPost
}

// renderSetup writes the GET /setup HTML. `formError` is rendered as
// a top-of-form error message; `values` pre-fills the form on a
// failed POST so the user doesn't lose their input.
func (h *Handler) renderSetup(w http.ResponseWriter, r *http.Request, st config.FirstRunStatus, formError error, values *config.ConfigBundle) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	data := setupPageData{
		Missing: st.Missing,
	}
	if values != nil {
		data.MarketAddress = values.Pdp.MarketAddress
		data.WalletAddress = values.Pdp.WalletAddress
		data.MinerID = values.Pdp.MinerID
	}
	if formError != nil {
		data.Error = formError.Error()
	}
	// If first-run is already complete we still render the page (so
	// an operator can revisit), but flag it so the template can show
	// a "already configured" hint.
	data.AlreadyConfigured = !st.NeedsSetup

	if err := setupTmpl.Execute(w, data); err != nil {
		// Can't really recover here — header is already flushed.
		// Best-effort fallback: write something so curl users see it.
		_, _ = fmt.Fprintln(w, "<!-- template render error: ", err, " -->")
	}
}

// submitSetup handles POST /api/setup.
func (h *Handler) submitSetup(w http.ResponseWriter, r *http.Request, st config.FirstRunStatus) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
		return
	}

	cfg := config.ConfigBundle{
		Pdp: config.PdpSection{
			MarketAddress: strings.TrimSpace(r.FormValue("market_address")),
			WalletAddress: strings.TrimSpace(r.FormValue("wallet_address")),
			MinerID:       strings.TrimSpace(r.FormValue("miner_id")),
		},
	}

	if err := config.UpsertDefaultLayer(r.Context(), h.db, cfg); err != nil {
		// Re-render the form with the error + the user's input.
		h.renderSetup(w, r, st, err, &cfg)
		return
	}

	// Re-check status so the redirect target is sensibly chosen
	// even if a second writer raced us.
	post, err := config.Status(r.Context(), h.db)
	if err == nil && post.NeedsSetup {
		// Someone wiped a field mid-flight; back to /setup.
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// setupPageData drives the setup HTML template.
type setupPageData struct {
	Missing           []string
	MarketAddress     string
	WalletAddress     string
	MinerID           string
	Error             string
	AlreadyConfigured bool
}

// setupTmpl is the entire /setup page. Inline HTML so the package
// has zero embed/FS surface; this is a single sub-100-line form and
// will stay that way until the Day 6 WebUI carve-out gives us a
// proper template tree.
var setupTmpl = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>curio-core · first-run setup</title>
  <style>
    body { font-family: -apple-system, system-ui, sans-serif; max-width: 540px; margin: 4em auto; padding: 0 1em; color: #1a1a1a; }
    h1 { font-size: 1.4em; margin-bottom: 0.2em; }
    p.lead { color: #555; margin-top: 0; }
    label { display: block; margin-top: 1.2em; font-weight: 600; }
    input[type=text] { width: 100%; padding: 0.55em 0.65em; border: 1px solid #bbb; border-radius: 4px; font-family: monospace; font-size: 0.95em; box-sizing: border-box; }
    button { margin-top: 1.6em; padding: 0.6em 1.4em; background: #1a4ed8; color: white; border: 0; border-radius: 4px; font-size: 1em; cursor: pointer; }
    button:hover { background: #1839a8; }
    .error { background: #fdecea; border: 1px solid #f5b8b1; color: #8a1f1f; padding: 0.8em; border-radius: 4px; margin-top: 1em; }
    .ok { background: #ecf6e8; border: 1px solid #b9d8a8; color: #2c5d1f; padding: 0.8em; border-radius: 4px; margin-top: 1em; }
    .hint { font-weight: 400; color: #888; font-size: 0.9em; }
  </style>
</head>
<body>
  <h1>curio-core · first-run setup</h1>
  <p class="lead">Configure the three identifiers curio-core needs before it can do any PDP work. You can come back later via <code>/setup</code> to update them.</p>

  {{if .AlreadyConfigured}}
    <div class="ok">All required fields are already configured. Submitting the form will overwrite the current values.</div>
  {{end}}

  {{if .Error}}
    <div class="error"><strong>Error:</strong> {{.Error}}</div>
  {{end}}

  <form method="POST" action="/api/setup">
    <label>Market address <span class="hint">(0x…)</span>
      <input type="text" name="market_address" required value="{{.MarketAddress}}" placeholder="0x...">
    </label>
    <label>Wallet address <span class="hint">(0x…)</span>
      <input type="text" name="wallet_address" required value="{{.WalletAddress}}" placeholder="0x...">
    </label>
    <label>Miner ID <span class="hint">(f0…)</span>
      <input type="text" name="miner_id" required value="{{.MinerID}}" placeholder="f0...">
    </label>
    <button type="submit">Save and continue</button>
  </form>
</body>
</html>`))

// Ensure the package isn't accidentally trimmed by the linker when
// the setup HTML stays embedded.
var _ = context.Background
