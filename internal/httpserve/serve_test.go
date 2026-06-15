package httpserve

import (
	"net/http"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
}

func TestBuildRejectsNilHandlers(t *testing.T) {
	if _, err := Build(Config{AdminListen: "127.0.0.1:0", PublicListen: "127.0.0.1:0", PublicHandler: okHandler()}); err == nil {
		t.Fatal("expected error for nil AdminHandler")
	}
	if _, err := Build(Config{AdminListen: "127.0.0.1:0", PublicListen: "127.0.0.1:0", AdminHandler: okHandler()}); err == nil {
		t.Fatal("expected error for nil PublicHandler")
	}
}

func TestBuildRejects443WithoutDomain(t *testing.T) {
	_, err := Build(Config{
		AdminListen:   "127.0.0.1:0",
		PublicListen:  "0.0.0.0:443",
		AdminHandler:  okHandler(),
		PublicHandler: okHandler(),
	})
	if err == nil {
		t.Fatal("expected error binding :443 without --public-tls-domain")
	}
	if !strings.Contains(err.Error(), "public-tls-domain") {
		t.Fatalf("error should mention public-tls-domain, got: %v", err)
	}
}

func TestBuildRejectsTLSWithoutCache(t *testing.T) {
	_, err := Build(Config{
		AdminListen:     "127.0.0.1:0",
		PublicListen:    "127.0.0.1:0",
		PublicTLSDomain: "sp.example.com",
		AdminHandler:    okHandler(),
		PublicHandler:   okHandler(),
		// Cache deliberately nil
	})
	if err == nil || !strings.Contains(err.Error(), "Cache") {
		t.Fatalf("expected Cache-required error, got: %v", err)
	}
}

func TestBuildPlaintextDevMode(t *testing.T) {
	// No domain + non-443 port => plaintext public listener, valid.
	s, err := Build(Config{
		AdminListen:   "127.0.0.1:0",
		PublicListen:  "127.0.0.1:0",
		AdminHandler:  okHandler(),
		PublicHandler: okHandler(),
	})
	if err != nil {
		t.Fatalf("plaintext dev build: %v", err)
	}
	defer func() {
		_ = s.AdminLn.Close()
		_ = s.PublicLn.Close()
	}()
	if s.TLSActive() {
		t.Fatal("TLSActive should be false in plaintext mode")
	}
	if !strings.HasPrefix(s.PublicURL(), "http://") {
		t.Fatalf("plaintext PublicURL should be http://, got %q", s.PublicURL())
	}
}

func TestIsPort443(t *testing.T) {
	cases := map[string]bool{
		"0.0.0.0:443":     true,
		"127.0.0.1:443":   true,
		"0.0.0.0:https":   true,
		"0.0.0.0:8080":    false,
		"127.0.0.1:14995": false,
		"0.0.0.0:80":      false,
	}
	for addr, want := range cases {
		if got := isPort443(addr); got != want {
			t.Errorf("isPort443(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestStripPort(t *testing.T) {
	if got := stripPort("sp.example.com:443"); got != "sp.example.com" {
		t.Errorf("stripPort = %q", got)
	}
	if got := stripPort("sp.example.com"); got != "sp.example.com" {
		t.Errorf("stripPort no-port = %q", got)
	}
}
