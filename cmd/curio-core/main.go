// Command curio-core is the PDP-only Curio + embedded Lantern bundle.
//
// Pre-alpha. The current binary exercises only the Lantern embedding:
// it starts an embedded daemon, prints the anchored chain head, and
// shuts down. This is the bones for the full integration that lands
// over subsequent commits — see docs/PLAN.md and Reiers/lantern#11.
//
// Subcommands (planned):
//
//	curio-core run    — start the full PDP node (Lantern + PDP tasks + DB)
//	curio-core init   — first-run wizard (wallet, miner ID, market addr)
//
// Today only `curio-core probe` works (single hard-coded smoke test).

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lantern "github.com/Reiers/lantern/pkg/daemon"
	"github.com/Reiers/lantern/wallet"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "probe":
		if err := cmdProbe(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core probe: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("curio-core 0.0.1-prealpha")
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `curio-core (pre-alpha)

  PDP-only Curio + embedded Lantern, single pure-Go binary.

Subcommands:
  probe          smoke-test the embedded Lantern daemon
  version        print version
  help           this message

Run 'curio-core probe' to confirm the embedded daemon anchors against
the Filecoin gateway and the Lantern <-> Curio Core boundary is intact.
`)
}

// cmdProbe boots an embedded Lantern daemon, prints the anchored head,
// then shuts down cleanly. Used to confirm the Lantern integration is
// wired correctly without running any of the (not-yet-written) Curio
// PDP tasks.
func cmdProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Local data directory (wallet + headerstore live here)")
	gateway := fs.String("gateway", "https://gateway.lantern.reiers.io", "Lantern gateway URL")
	timeout := fs.Duration("timeout", 30*time.Second, "Total probe timeout")
	fs.Parse(args)

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	fmt.Printf("curio-core probe: starting embedded Lantern daemon\n")
	fmt.Printf("  data-dir: %s\n", *dataDir)
	fmt.Printf("  gateway:  %s\n", *gateway)
	fmt.Printf("  timeout:  %s\n", *timeout)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Wallet is required by daemon.Config; an empty wallet is fine for
	// the probe since we're not signing anything.
	w, err := wallet.New(ctx, filepath.Join(*dataDir, "keystore"), "")
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}

	d, err := lantern.New(lantern.Config{
		DataDir:      *dataDir,
		Wallet:       w,
		Gateway:      *gateway,
		NoLibp2p:     true,
		EmbeddedMode: true,
	})
	if err != nil {
		return fmt.Errorf("new daemon: %w", err)
	}

	// Run Start in a goroutine; print results as soon as it's Started.
	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()

	// Wait for Started or fatal error.
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		if d.Started() {
			break
		}
		select {
		case err := <-errCh:
			return fmt.Errorf("daemon exited before Started: %w", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !d.Started() {
		return fmt.Errorf("daemon did not reach Started state within %s", *timeout)
	}

	tr := d.TrustedRoot()
	fmt.Printf("\nAnchored:\n")
	fmt.Printf("  epoch:       %d\n", tr.Epoch)
	fmt.Printf("  state root:  %s\n", tr.StateRoot)
	if tr.F3Instance > 0 {
		fmt.Printf("  F3 instance: %d\n", tr.F3Instance)
	}
	fmt.Printf("  anchored at: %s\n", tr.AcceptedAt.UTC().Format(time.RFC3339))

	// Also honour SIGINT/SIGTERM so a Ctrl-C exits cleanly even if the
	// probe timeout hasn't fired yet.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("\nProbe successful. Shutting down embedded daemon...\n")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := d.Stop(stopCtx); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	select {
	case <-errCh:
	case <-sig:
	}
	fmt.Printf("Stopped cleanly.\n")
	return nil
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/curio-core"
	}
	return filepath.Join(home, ".curio-core")
}
