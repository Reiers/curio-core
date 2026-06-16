// Package httpserve runs curio-core's two-port HTTP model (curio-core#69),
// removing the nginx-in-front dependency from the default deploy.
//
// Port A — admin / UI (default loopback, no TLS):
//
//	dashboard, /setup, /admin/*. Operator surface. Never exposed
//	publicly by default.
//
// Port B — public synapse-sdk surface:
//
//	/pdp/* + /piece/*. TLS via golang.org/x/crypto/acme/autocert when a
//	domain is configured, with the ACME HTTP-01 challenge served on :80.
//	Plaintext only in explicit dev mode (no domain). Binding :443 without
//	a domain is refused.
//
// The cert + ACME account state persist in the SQLite autocert_cache
// table via internal/acmecache, so restarts don't trigger a fresh ACME
// round-trip.
package httpserve

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// Config configures the two-port server set.
type Config struct {
	// AdminListen is the loopback bind for the operator UI/admin surface
	// (e.g. "127.0.0.1:4711").
	AdminListen string

	// PublicListen is the bind for the public synapse-sdk + retrieval
	// surface (e.g. "0.0.0.0:443" in prod, "127.0.0.1:14995" behind an
	// existing proxy, or "0.0.0.0:8080" in dev).
	PublicListen string

	// PublicTLSDomain, when non-empty, enables autocert TLS for the
	// public listener and provisions a LetsEncrypt cert for this domain.
	// When empty the public listener serves plaintext (dev only) and
	// binding a :443 PublicListen is refused.
	PublicTLSDomain string

	// ACMEHTTPChallengeAddr is the bind for the ACME HTTP-01 challenge +
	// HTTP->HTTPS redirect listener (default ":80"). Only used when
	// PublicTLSDomain is set. Empty disables the :80 listener (operator
	// is handling HTTP-01 elsewhere, e.g. DNS-01 in future).
	ACMEHTTPChallengeAddr string

	// ACMEDirectoryURL overrides the ACME directory (for staging/tests).
	// Empty uses autocert's default (LetsEncrypt production).
	ACMEDirectoryURL string

	// AdminHandler serves the loopback admin/UI surface.
	AdminHandler http.Handler

	// PublicHandler serves the public /pdp/* + /piece/* surface.
	PublicHandler http.Handler

	// Cache persists ACME state. Required when PublicTLSDomain is set.
	Cache autocert.Cache
}

// Servers holds the constructed listeners + http.Servers so the caller
// owns their lifecycle (Serve in goroutines, Shutdown on signal).
type Servers struct {
	Admin     *http.Server
	AdminLn   net.Listener
	Public    *http.Server
	PublicLn  net.Listener
	ACME      *http.Server // nil when TLS disabled or challenge addr empty
	acmeLn    net.Listener // bound ACME HTTP-01 listener (nil when ACME nil)
	tlsActive bool
	domain    string
}

// TLSActive reports whether the public listener serves TLS.
func (s *Servers) TLSActive() bool { return s.tlsActive }

// PublicURL returns a human-facing URL for the public surface.
func (s *Servers) PublicURL() string {
	if s.tlsActive {
		return "https://" + s.domain + "/"
	}
	if s.PublicLn != nil {
		return "http://" + s.PublicLn.Addr().String() + "/"
	}
	return ""
}

const (
	defaultReadHeaderTimeout = 10 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 0 // 0 = no write timeout; piece retrieval streams large bodies
)

// isPort443 reports whether addr binds the standard HTTPS port.
func isPort443(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No explicit port — only ":443" style would have parsed; treat
		// a bare host as not-443.
		return false
	}
	return port == "443" || port == "https"
}

// Build validates Config and constructs (but does not Serve) the
// two-port server set, binding the listeners so bind errors surface
// before the caller commits to the run loop.
func Build(cfg Config) (*Servers, error) {
	if cfg.AdminHandler == nil {
		return nil, errors.New("httpserve: AdminHandler is nil")
	}
	if cfg.PublicHandler == nil {
		return nil, errors.New("httpserve: PublicHandler is nil")
	}
	if cfg.AdminListen == "" {
		return nil, errors.New("httpserve: AdminListen is empty")
	}
	if cfg.PublicListen == "" {
		return nil, errors.New("httpserve: PublicListen is empty")
	}

	tlsEnabled := cfg.PublicTLSDomain != ""

	// Refuse :443 without a domain — serving plaintext on the HTTPS port
	// is an operator footgun.
	if !tlsEnabled && isPort443(cfg.PublicListen) {
		return nil, fmt.Errorf("httpserve: refusing to bind %q without --public-tls-domain (set a domain for TLS, or pick a non-443 port for plaintext dev)", cfg.PublicListen)
	}
	if tlsEnabled && cfg.Cache == nil {
		return nil, errors.New("httpserve: PublicTLSDomain set but Cache is nil")
	}

	s := &Servers{domain: cfg.PublicTLSDomain}

	// --- Admin server (loopback, no TLS) ---
	adminLn, err := net.Listen("tcp", cfg.AdminListen)
	if err != nil {
		return nil, fmt.Errorf("httpserve: admin listen %q: %w", cfg.AdminListen, err)
	}
	s.AdminLn = adminLn
	s.Admin = &http.Server{
		Handler:           cfg.AdminHandler,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
	}

	// --- Public server ---
	publicLn, err := net.Listen("tcp", cfg.PublicListen)
	if err != nil {
		_ = adminLn.Close()
		return nil, fmt.Errorf("httpserve: public listen %q: %w", cfg.PublicListen, err)
	}
	s.PublicLn = publicLn
	s.Public = &http.Server{
		Handler:           cfg.PublicHandler,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
	}

	if tlsEnabled {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.PublicTLSDomain),
			Cache:      cfg.Cache,
		}
		if cfg.ACMEDirectoryURL != "" {
			m.Client = &acme.Client{DirectoryURL: cfg.ACMEDirectoryURL}
		}
		tlsCfg := m.TLSConfig()
		// Ensure modern minimum; autocert sets NextProtos already.
		tlsCfg.MinVersion = tls.VersionTLS12
		s.Public.TLSConfig = tlsCfg
		s.tlsActive = true

		// ACME HTTP-01 challenge + HTTP->HTTPS redirect on :80.
		if cfg.ACMEHTTPChallengeAddr != "" {
			acmeLn, err := net.Listen("tcp", cfg.ACMEHTTPChallengeAddr)
			if err != nil {
				_ = adminLn.Close()
				_ = publicLn.Close()
				return nil, fmt.Errorf("httpserve: ACME HTTP-01 listen %q: %w", cfg.ACMEHTTPChallengeAddr, err)
			}
			s.ACME = &http.Server{
				Handler:           m.HTTPHandler(redirectToHTTPS(cfg.PublicTLSDomain)),
				ReadHeaderTimeout: defaultReadHeaderTimeout,
			}
			// Stash the listener on the server via BaseContext closure is
			// awkward; keep it simple and serve from Run using a field.
			s.acmeLn = acmeLn
		}
	}

	return s, nil
}

// Run serves all listeners. It blocks until ctx is cancelled or a
// listener returns a fatal error, then gracefully shuts down. The
// returned error is the first fatal serve error, or nil on clean
// ctx-driven shutdown.
func (s *Servers) Run(ctx context.Context) error {
	serveErr := make(chan error, 3)

	go func() {
		err := s.Admin.Serve(s.AdminLn)
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("admin serve: %w", err)
			return
		}
		serveErr <- nil
	}()

	go func() {
		var err error
		if s.tlsActive {
			// Cert comes from autocert via TLSConfig.GetCertificate.
			err = s.Public.ServeTLS(s.PublicLn, "", "")
		} else {
			err = s.Public.Serve(s.PublicLn)
		}
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("public serve: %w", err)
			return
		}
		serveErr <- nil
	}()

	if s.ACME != nil && s.acmeLn != nil {
		go func() {
			err := s.ACME.Serve(s.acmeLn)
			if !errors.Is(err, http.ErrServerClosed) {
				serveErr <- fmt.Errorf("acme serve: %w", err)
				return
			}
			serveErr <- nil
		}()
	}

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-serveErr:
		_ = s.shutdown()
		return err
	}
}

func (s *Servers) shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var firstErr error
	for _, srv := range []*http.Server{s.ACME, s.Public, s.Admin} {
		if srv == nil {
			continue
		}
		if err := srv.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// redirectToHTTPS is the fallback handler behind autocert's HTTP-01
// handler: any non-challenge HTTP request is 301'd to its HTTPS
// equivalent on the configured domain.
func redirectToHTTPS(domain string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := domain
		if host == "" {
			host = stripPort(r.Host)
		}
		target := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}

func stripPort(hostport string) string {
	if i := strings.IndexByte(hostport, ':'); i >= 0 {
		return hostport[:i]
	}
	return hostport
}
