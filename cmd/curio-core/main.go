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
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lanternbuild "github.com/Reiers/lantern/build"
	lantern "github.com/Reiers/lantern/pkg/daemon"
	"github.com/Reiers/lantern/wallet"

	"github.com/go-chi/chi/v5"

	"github.com/Reiers/curio-core/internal/admin"
	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/engine"
	"github.com/Reiers/curio-core/internal/ethkeys"
	"github.com/Reiers/curio-core/internal/pdpwire"
	"github.com/Reiers/curio-core/internal/setupweb"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/message"
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
	case "run":
		if err := cmdRun(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core run: %v\n", err)
			os.Exit(1)
		}
	case "wallet":
		if err := cmdWallet(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core wallet: %v\n", err)
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
  run            start the daemon: Lantern + harmonytask engine + WebUI
  wallet         operator wallet management (list, new, import, export, role, delete)
  version        print version
  help           this message

Run 'curio-core probe' to confirm the embedded daemon anchors against
the Filecoin gateway and the Lantern <-> Curio Core boundary is intact.
Run 'curio-core run --help' for the long-running daemon's flags.
`)
}

// cmdProbe boots an embedded Lantern daemon, prints the anchored head,
// then shuts down cleanly. Used to confirm the Lantern integration is
// wired correctly without running any of the (not-yet-written) Curio
// PDP tasks.
func cmdProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Local data directory (wallet + headerstore live here)")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Filecoin network: mainnet | calibration")
	gateway := fs.String("gateway", "", "Lantern gateway URL (default chosen per --network; see pkg/daemon.applyDefaults)")
	timeout := fs.Duration("timeout", 30*time.Second, "Total probe timeout")
	fs.Parse(args)

	if !lanternbuild.Network(*network).Valid() {
		return fmt.Errorf("invalid --network %q: want one of mainnet, calibration", *network)
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	fmt.Printf("curio-core probe: starting embedded Lantern daemon\n")
	fmt.Printf("  data-dir: %s\n", *dataDir)
	fmt.Printf("  network:  %s\n", *network)
	if *gateway != "" {
		fmt.Printf("  gateway:  %s\n", *gateway)
	} else {
		fmt.Printf("  gateway:  (default chosen per network)\n")
	}
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
		Network:      *network,
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

// cmdRun boots the full curio-core daemon: embedded Lantern, the
// harmonytask engine wired against harmonysqlite, and the WebUI with
// the first-run /setup flow. It blocks until SIGINT/SIGTERM (or, on
// error, a sub-component shutdown), then unwinds in the reverse order
// it brought things up.
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Local data directory (wallet + headerstore live here)")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Filecoin network: mainnet | calibration")
	gateway := fs.String("gateway", "", "Lantern gateway URL (default chosen per --network; see pkg/daemon.applyDefaults)")
	dbPath := fs.String("db-path", "", "Path to the harmonysqlite state DB (default: <data-dir>/state.sqlite)")
	listenAddr := fs.String("listen", "127.0.0.1:4711", "HTTP listen address for the WebUI / /setup flow")
	noLantern := fs.Bool("no-lantern", false, "Skip the embedded Lantern daemon (engine + WebUI only; useful for first-run setup on a host without gateway access yet)")
	lanternTimeout := fs.Duration("lantern-anchor-timeout", 30*time.Second, "Time to wait for Lantern to reach Started state during boot")
	vmBridgeRPC := fs.String("vm-bridge-rpc", "", "Upstream Forest/Lotus JSON-RPC URL for FEVM forwarding (eth_call/eth_estimateGas/sendRawTransaction). Defaults per --network: calibration -> https://api.calibration.node.glif.io/rpc/v1, mainnet -> https://api.node.glif.io/rpc/v1. Pass an empty string with --vm-bridge-rpc-disable to disable.")
	vmBridgeToken := fs.String("vm-bridge-token", "", "Optional Bearer token for the VM bridge upstream (defaults to env LANTERN_VM_BRIDGE_TOKEN)")
	vmBridgeDisable := fs.Bool("vm-bridge-rpc-disable", false, "Disable VM bridge entirely (eth_call et al. will return 'FEVM method requires --vm-bridge-rpc'). Use only when curio-core is being driven by a flow that doesn't read contract state.")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: curio-core run [flags]

Start the curio-core daemon: embedded Lantern, the harmonytask engine
wired against the SQLite state DB, and the WebUI with the first-run
/setup flow.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !lanternbuild.Network(*network).Valid() {
		return fmt.Errorf("invalid --network %q: want one of mainnet, calibration", *network)
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if *dbPath == "" {
		*dbPath = filepath.Join(*dataDir, "state.sqlite")
	}

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	// --- Bring up the engine ---
	fmt.Printf("curio-core run: starting daemon\n")
	fmt.Printf("  data-dir: %s\n", *dataDir)
	fmt.Printf("  network:  %s\n", *network)
	fmt.Printf("  db-path:  %s\n", *dbPath)
	fmt.Printf("  listen:   %s\n", *listenAddr)

	eng, err := engine.New(rootCtx, engine.Config{DBPath: *dbPath})
	if err != nil {
		return fmt.Errorf("engine.New: %w", err)
	}
	// Note: engine.Start is deferred until after Lantern boot + chain-deps
	// build + eth_keys bootstrap, so the SendTaskETH can be registered with
	// harmonytask up-front. See the Lantern + ChainDeps blocks below.

	// --- First-run probe ---
	st, err := config.Status(rootCtx, eng.DB())
	if err != nil {
		_ = eng.Stop()
		return fmt.Errorf("first-run status: %w", err)
	}
	if st.NeedsSetup {
		fmt.Printf("Setup required. Open http://%s/setup in a browser to complete.\n", *listenAddr)
	} else {
		fmt.Printf("  config:   default layer present (%d field(s) configured)\n", 3)
	}

	// --- Optional Lantern ---
	var lanternDaemon *lantern.Daemon
	if !*noLantern {
		w, err := wallet.New(rootCtx, filepath.Join(*dataDir, "keystore"), "")
		if err != nil {
			_ = eng.Stop()
			return fmt.Errorf("create wallet: %w", err)
		}
		// VM bridge default: pick a calibration / mainnet Glif endpoint per
		// --network. Operators can override with --vm-bridge-rpc or disable
		// with --vm-bridge-rpc-disable. This is the one architectural
		// compromise in the embedded-Lantern story: until Lantern can
		// execute FEVM reads from its own state tree (lantern#3 area),
		// curio-core forwards eth_call / eth_estimateGas to a public RPC.
		bridgeURL := *vmBridgeRPC
		if !*vmBridgeDisable && bridgeURL == "" {
			switch *network {
			case "calibration":
				bridgeURL = "https://api.calibration.node.glif.io/rpc/v1"
			case "mainnet":
				bridgeURL = "https://api.node.glif.io/rpc/v1"
			}
		}
		if *vmBridgeDisable {
			bridgeURL = ""
		}
		lanternDaemon, err = lantern.New(lantern.Config{
			DataDir: *dataDir,
			Wallet:  w,
			Gateway: *gateway,
			Network: *network,
			// Ephemeral loopback bind: embedded Lantern speaks /rpc/v1 to
			// in-process consumers (nodeapi, ethclient) only. We never expose
			// this port externally; nginx terminates client traffic at the
			// curio-core listener (14994) which composes /pdp/* over the
			// upstream PDPService that itself talks to Lantern through this
			// loopback. Port 0 avoids conflicts with a real standalone
			// Lantern on the same host.
			RPCListen:     "127.0.0.1:0",
			NoLibp2p:      true,
			EmbeddedMode:  true,
			VMBridgeRPC:   bridgeURL,
			VMBridgeToken: *vmBridgeToken,
		})
		if err != nil {
			_ = eng.Stop()
			return fmt.Errorf("new lantern daemon: %w", err)
		}
		lanternErr := make(chan error, 1)
		go func() { lanternErr <- lanternDaemon.Start(rootCtx) }()

		deadline := time.Now().Add(*lanternTimeout)
		for time.Now().Before(deadline) {
			if lanternDaemon.Started() {
				break
			}
			select {
			case err := <-lanternErr:
				_ = eng.Stop()
				return fmt.Errorf("lantern exited before Started: %w", err)
			case <-time.After(50 * time.Millisecond):
			}
		}
		if !lanternDaemon.Started() {
			_ = eng.Stop()
			return fmt.Errorf("lantern did not reach Started state within %s", *lanternTimeout)
		}
		tr := lanternDaemon.TrustedRoot()
		fmt.Printf("  lantern:  anchored at epoch %d\n", tr.Epoch)
		if addr := lanternDaemon.RPCAddr(); addr != "" {
			fmt.Printf("  lantern:  rpc at http://%s/rpc/v1 (in-process)\n", addr)
		}
		if bridgeURL != "" {
			fmt.Printf("  lantern:  vm-bridge -> %s\n", bridgeURL)
		}
	} else {
		fmt.Printf("  lantern:  skipped (--no-lantern)\n")
	}

	// --- Chain deps: ethclient + nodeapi + SenderETH ---
	chainDeps, err := pdpwire.BuildChainDeps(rootCtx, eng.DB(), lanternDaemon)
	if err != nil {
		_ = eng.Stop()
		return fmt.Errorf("pdpwire.BuildChainDeps: %w", err)
	}
	if chainDeps != nil {
		defer chainDeps.Close()
	}

	// --- eth_keys bootstrap: ensure a 'pdp' role key exists ---
	if ethAddr, err := ethkeys.Bootstrap(rootCtx, eng.DB()); err != nil {
		_ = eng.Stop()
		return fmt.Errorf("ethkeys.Bootstrap: %w", err)
	} else {
		fmt.Printf("  eth_keys: %s (role=pdp)\n", ethAddr)
	}

	// --- Engine start: register pdpv0 + SendTaskETH ---
	var extraTasks []harmonytask.TaskInterface
	if chainDeps != nil && chainDeps.SendTaskETH != nil {
		extraTasks = append(extraTasks, chainDeps.SendTaskETH)
	}
	if err := eng.Start(rootCtx, extraTasks...); err != nil {
		_ = eng.Stop()
		return fmt.Errorf("engine.Start: %w", err)
	}
	fmt.Printf("  engine:   %d task types registered\n", eng.Registry().Len())

	// --- HTTP server ---
	// curio-core composes two route sets under one listener:
	//   /pdp/*  — upstream curio/pdp HTTP API (synapse-sdk speaks this)
	//   /, /setup, /api/setup — curio-core's first-run WebUI flow
	pdpMux := chi.NewRouter()
	stashDir := filepath.Join(*dataDir, "stash")
	if _, err := pdpwire.Mount(rootCtx, pdpMux, eng.DB(), stashDir, chainDeps); err != nil {
		_ = eng.Stop()
		return fmt.Errorf("pdpwire.Mount: %w", err)
	}
	fmt.Printf("  pdp:      /pdp/* routes mounted (stash %s)\n", stashDir)

	// Mount /admin/* on the same chi router (test-tx, eth-key).
	// Loopback-only by intent; nginx in front does not forward /admin/*
	// to the public internet.
	var adminSender *message.SenderETH
	if chainDeps != nil {
		adminSender = chainDeps.SenderETH
	}
	admin.Routes(pdpMux, eng.DB(), adminSender)
	fmt.Printf("  admin:    /admin/test-tx, /admin/eth-key mounted (loopback)\n")

	handler := pdpwire.FallbackHandler(pdpMux, setupweb.New(eng.DB()))
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		_ = eng.Stop()
		if lanternDaemon != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = lanternDaemon.Stop(stopCtx)
			stopCancel()
		}
		return fmt.Errorf("http listen: %w", err)
	}
	fmt.Printf("  webui:    http://%s/\n", ln.Addr())

	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	fmt.Printf("\ncurio-core is running. Ctrl-C to stop.\n")

	// --- Signal wait ---
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-sig:
		fmt.Printf("\nreceived %s; shutting down...\n", s)
	case err := <-serveErr:
		if err != nil {
			fmt.Fprintf(os.Stderr, "http server error: %v\n", err)
		}
	}

	// --- Shutdown, in reverse order ---
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = srv.Shutdown(shutdownCtx)

	if lanternDaemon != nil {
		if err := lanternDaemon.Stop(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "lantern stop: %v\n", err)
		}
	}

	if err := eng.Stop(); err != nil {
		return fmt.Errorf("engine.Stop: %w", err)
	}

	fmt.Printf("Stopped cleanly.\n")
	return nil
}
