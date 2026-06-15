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
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"

	lanternbuild "github.com/Reiers/lantern/build"
	lantern "github.com/Reiers/lantern/pkg/daemon"
	"github.com/Reiers/lantern/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/pdp/contract"

	"github.com/go-chi/chi/v5"

	"github.com/Reiers/curio-core/internal/acmecache"
	"github.com/Reiers/curio-core/internal/admin"
	"github.com/Reiers/curio-core/internal/alerts"
	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/dashboard"
	"github.com/Reiers/curio-core/internal/engine"
	"github.com/Reiers/curio-core/internal/ethkeys"
	"github.com/Reiers/curio-core/internal/httpserve"
	"github.com/Reiers/curio-core/internal/parkcomplete"
	"github.com/Reiers/curio-core/internal/payments"
	"github.com/Reiers/curio-core/internal/pdpwire"
	"github.com/Reiers/curio-core/internal/retrieval"
	"github.com/Reiers/curio-core/internal/setupweb"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/tasks/message"
)

// versionTag is set at build time via -ldflags "-X main.versionTag=<tag>".
// Falls back to the baked-in pre-alpha string for `go build`/`go run`.
var versionTag = "0.0.1-prealpha"

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
	case "doctor":
		if err := cmdDoctor(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core doctor: %v\n", err)
			os.Exit(1)
		}
	case "sp":
		if err := cmdSP(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core sp: %v\n", err)
			os.Exit(1)
		}
	case "config":
		if err := cmdConfig(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core config: %v\n", err)
			os.Exit(1)
		}
	case "demo":
		if err := cmdDemo(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core demo: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Printf("curio-core %s\n", versionTag)
	case "upgrade":
		if err := cmdUpgrade(args); err != nil {
			fmt.Fprintf(os.Stderr, "curio-core upgrade: %v\n", err)
			os.Exit(1)
		}
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
  doctor         read-only health + DB ↔ chain reconciliation report
  sp             SP registry operations (register, info)
  config         headless config inspection + mutation
  demo           synapse-sdk-shaped end-to-end demo flows (create-dataset)
  upgrade        check GitHub releases for a newer version (opt-in self-update)
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
		DataDir: *dataDir,
		Wallet:  w,
		Gateway: *gateway,
		Network: *network,
		// Probe is short-lived + read-only: polling head is fine, no
		// reason to mount a libp2p host (curio-core#74 keeps gossipsub
		// for the long-running daemon only).
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
	listenAddr := fs.String("listen", "127.0.0.1:4711", "[deprecated alias for --admin-listen] HTTP listen for the admin WebUI / /setup flow")
	adminListen := fs.String("admin-listen", "", "Admin/UI loopback listen (dashboard, /setup, /admin/*). Defaults to --listen. Keep this loopback-only.")
	publicListen := fs.String("public-listen", "", "Public synapse-sdk surface listen (/pdp/*, /piece/*). Empty disables the public port (single-port admin-only mode). Use 0.0.0.0:443 with --public-tls-domain for prod, or 127.0.0.1:14995 behind an existing proxy.")
	publicTLSDomain := fs.String("public-tls-domain", "", "Domain for autocert (LetsEncrypt) TLS on the public port. Empty serves plaintext (dev only) and refuses to bind :443.")
	acmeHTTPAddr := fs.String("acme-http-listen", ":80", "Bind for the ACME HTTP-01 challenge + HTTP->HTTPS redirect (only used with --public-tls-domain). Empty disables the :80 listener.")
	acmeDirectoryURL := fs.String("acme-directory-url", "", "Override the ACME directory URL (for LetsEncrypt staging / tests). Empty uses production.")
	noLantern := fs.Bool("no-lantern", false, "Skip the embedded Lantern daemon (engine + WebUI only; useful for first-run setup on a host without gateway access yet)")
	lanternTimeout := fs.Duration("lantern-anchor-timeout", 30*time.Second, "Time to wait for Lantern to reach Started state during boot")
	vmBridgeRPC := fs.String("vm-bridge-rpc", "", "Upstream Forest/Lotus JSON-RPC URL for FEVM forwarding (eth_call/eth_estimateGas/sendRawTransaction). Defaults per --network: calibration -> https://api.calibration.node.glif.io/rpc/v1, mainnet -> https://api.node.glif.io/rpc/v1. Pass an empty string with --vm-bridge-rpc-disable to disable.")
	vmBridgeToken := fs.String("vm-bridge-token", "", "Optional Bearer token for the VM bridge upstream (defaults to env LANTERN_VM_BRIDGE_TOKEN)")
	vmBridgeDisable := fs.Bool("vm-bridge-rpc-disable", false, "Disable VM bridge entirely (eth_call et al. will return 'FEVM method requires --vm-bridge-rpc'). Use only when curio-core is being driven by a flow that doesn't read contract state.")
	noLibp2p := fs.Bool("no-libp2p", false, "Disable the embedded Lantern libp2p host (gossipsub head-tracking). Head-following falls back to gateway polling only. Use on hosts where outbound p2p is blocked or unwanted.")
	p2pListen := fs.String("p2p-listen", "", "Comma-separated libp2p listen multiaddrs for gossipsub head-tracking (default: /ip4/0.0.0.0/tcp/0,/ip4/0.0.0.0/udp/0/quic-v1)")
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
			RPCListen: "127.0.0.1:0",
			// Gossipsub head-tracking ON by default for the long-running
			// daemon (curio-core#74). The libp2p host + DHT + block ingestor
			// track head over /fil/blocks/<network> at 0-1 epoch latency;
			// the polling Sync drops to a relaxed 30s catch-up fallback.
			// This closes the head-staleness class behind curio-core#62
			// (lantern#33/#40). Opt out with --no-libp2p.
			NoLibp2p:      *noLibp2p,
			P2PListen:     *p2pListen,
			EmbeddedMode:  true,
			VMBridgeRPC:   bridgeURL,
			VMBridgeToken: *vmBridgeToken,

			// lantern#44: warm the embedded blockstore on every head
			// advance for the contracts curio-core reads. This is what
			// lets local eth_call serve PDPVerifier / FWSS / SP-registry
			// / USDFC reads from the cache instead of falling back to
			// the VMBridge. Unknown networks (devnet) get nil and the
			// prefetcher is a no-op.
			FEVMPrefetchAddrs: fevmPrefetchAddrsForNetwork(*network),
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
		if _, ok := lanternDaemon.GossipStats(); ok {
			fmt.Printf("  lantern:  gossipsub head-tracking ON (/fil/blocks/%s; polling sync relaxed to catch-up fallback)\n", *network)
		} else {
			fmt.Printf("  lantern:  gossipsub head-tracking OFF (gateway polling only)\n")
		}
	} else {
		fmt.Printf("  lantern:  skipped (--no-lantern)\n")
	}

	// stashDir is the directory diskstash writes streaming-upload
	// files into. Hoisted here so BuildChainDeps can construct the
	// local piece-park reader (which serves the same files to
	// cachedreader's piece-park fallback path).
	stashDir := filepath.Join(*dataDir, "stash")

	// --- Chain deps: ethclient + nodeapi + SenderETH + the proof loop ---
	chainDeps, err := pdpwire.BuildChainDeps(rootCtx, eng.DB(), stashDir, lanternDaemon)
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

	// --- Engine start: register pdpv0 + SendTaskETH + ChainSync + ParkComplete + proof-loop tasks ---
	var extraTasks []harmonytask.TaskInterface
	if chainDeps != nil && chainDeps.SendTaskETH != nil {
		extraTasks = append(extraTasks, chainDeps.SendTaskETH)
	}
	if chainDeps != nil && chainDeps.ChainSync != nil {
		extraTasks = append(extraTasks, chainDeps.ChainSync)
	}
	if chainDeps != nil && chainDeps.SaveCache != nil {
		extraTasks = append(extraTasks, chainDeps.SaveCache)
	}
	if chainDeps != nil && chainDeps.ProveTask != nil {
		extraTasks = append(extraTasks, chainDeps.ProveTask)
	}
	if chainDeps != nil && chainDeps.InitProvingPeriodTask != nil {
		extraTasks = append(extraTasks, chainDeps.InitProvingPeriodTask)
	}
	if chainDeps != nil && chainDeps.NextProvingPeriodTask != nil {
		extraTasks = append(extraTasks, chainDeps.NextProvingPeriodTask)
	}
	// ParkComplete: curio-core streaming-upload-specific completion
	// task. Flips parked_pieces.complete=TRUE for pieces whose bytes
	// landed in diskstash. Upstream's ParkPieceTask does this via
	// ffi.SealCalls + paths.Remote (cluster-aware bytes-copy); we
	// don't need that because stash IS our long-term storage.
	// See internal/parkcomplete for the rationale.
	// Wake-at-write (#67): pass eng.NotifyKick so a completed piece wakes
	// PDPv0_Notify inline instead of waiting for its poll cycle. NotifyKick
	// is nil-safe before engine.Start constructs the notify task, and is
	// only ever *invoked* at runtime (during parkComplete.Do), well after
	// Start, so the deferred indirection is safe.
	parkComplete := parkcomplete.NewWithWake(eng.DB(), stashDir, eng.NotifyKick)
	extraTasks = append(extraTasks, parkComplete)

	// PaymentSettle: discovers + settles USDFC payment rails for the
	// PDP-as-SP role. FilecoinWarmStorageService creates one rail per
	// client/dataset; we must call settleRail to claim accrued USDFC.
	// Without this task, USDFC stays locked in FilecoinPay and never
	// reaches our balance. See internal/payments.
	if chainDeps != nil && chainDeps.EthClient != nil && chainDeps.SenderETH != nil {
		payeeHex, lookupErr := ethkeys.LookupPDP(rootCtx, eng.DB())
		if lookupErr != nil || payeeHex == "" {
			fmt.Printf("  payments: skipped (no eth_keys role=pdp: %v)\n", lookupErr)
		} else {
			payContractNet := contract.Network(*network)
			settleTask, settleErr := payments.New(
				eng.DB(), chainDeps.EthClient, chainDeps.SenderETH,
				payContractNet, common.HexToAddress(payeeHex),
			)
			if settleErr != nil {
				fmt.Printf("  payments: skipped (%v)\n", settleErr)
			} else {
				extraTasks = append(extraTasks, settleTask)
				fmt.Printf("  payments: USDFC rail settler active (every %s, payee=%s)\n",
					payments.PollInterval, payeeHex)
			}
		}
	}
	// Install the tipset-subscription scheduler BEFORE engine.Start.
	// BuildChainDeps already registered the three pdpv0 watcher
	// handlers on it (DataSetWatch, TerminateServiceWatcher,
	// DataSetDeleteWatcher); the engine takes ownership of Run() +
	// shutdown cancellation.
	if chainDeps != nil && chainDeps.ChainSched != nil {
		eng.SetChainSched(chainDeps.ChainSched)

		// Wire MessageWatcherEth in the before-chainSched window. It polls
		// message_waits_eth 'pending' rows, fetches receipts via the
		// embedded Lantern VMBridge, and marks tx confirmed so the pdpv0
		// dataset_watch / terminate / delete handlers can advance past
		// `ok IS NULL` rows. It needs the live TaskEngine ref AND must
		// register its chainSched watcher BEFORE chainSched.Run() starts
		// (AddWatcher rejects registration after start). Constructing it
		// after eng.Start() — as before — raced the scheduler and failed
		// with "cannot add watcher handler after start", silently dropping
		// eth tx confirmation (curio-core#81).
		if chainDeps.EthClient != nil {
			ethClient := chainDeps.EthClient
			eng.OnBeforeChainSched(func(te *harmonytask.TaskEngine, sched *chainsched.CurioChainSched) error {
				if _, err := message.NewMessageWatcherEth(eng.DB(), te, sched, ethClient); err != nil {
					return fmt.Errorf("NewMessageWatcherEth: %w", err)
				}
				return nil
			})
		}
	}
	if err := eng.Start(rootCtx, extraTasks...); err != nil {
		_ = eng.Stop()
		return fmt.Errorf("engine.Start: %w", err)
	}
	// extraTasks holds the live TaskInterface implementations we threaded
	// in alongside the built-in PDPNotify task. Total live impls = 1 +
	// len(extraTasks). The Registry().Len() value below is the count of
	// static TaskTypeDetails descriptors curio-core knows about (the
	// PDPv0 task surface), which is a different number from the live
	// impls because not every descriptor has an active impl in every
	// deployment (e.g. PullPiece is on the descriptor list but not wired).
	liveImpls := 1 + len(extraTasks)
	fmt.Printf("  engine:   %d live task impls, %d descriptor entries\n",
		liveImpls, eng.Registry().Len())
	if chainDeps != nil && chainDeps.ChainSched != nil {
		fmt.Printf("  watchers: pdpv0 dataset/terminate/delete handlers wired on tipset sub\n")
	}
	fmt.Printf("  parkcomplete: streaming-upload -> parked_pieces.complete bridge active (stash=%s)\n", stashDir)

	if chainDeps != nil && chainDeps.ChainSched != nil && chainDeps.EthClient != nil {
		// Wired pre-Start via OnBeforeChainSched above; just report it.
		fmt.Printf("  msg-watch: message_waits_eth pending-tx poller active\n")
	}

	// Alerts (Reiers/curio-core#48): start the harmony_task_history poller
	// that translates task failures into deduped alerts at /admin/alerts.
	// Polls every 30s; bounded work per tick. Best-effort, non-blocking on
	// the main lifecycle.
	alertsPoller := alerts.NewPoller(eng.DB(), 30*time.Second)
	go func() {
		if err := alertsPoller.Run(rootCtx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Printf("  alerts: poller exited with error: %v\n", err)
		}
	}()
	fmt.Printf("  alerts:   /admin/alerts active (task-history poller, 30s interval)\n")

	// --- HTTP server (two-port model, curio-core#69) ---
	// Public surface (TLS-capable, internet-facing):
	//   /pdp/*   — upstream curio/pdp HTTP API (synapse-sdk speaks this)
	//   /piece/* — HTTP retrieval (PDP read path)
	// Admin surface (loopback, no TLS):
	//   /admin/* — test-tx, eth-key
	//   /, /setup, /api/setup, dashboard — operator UI
	publicMux := chi.NewRouter()
	if _, err := pdpwire.Mount(rootCtx, publicMux, eng.DB(), stashDir, chainDeps); err != nil {
		_ = eng.Stop()
		return fmt.Errorf("pdpwire.Mount: %w", err)
	}
	fmt.Printf("  pdp:      /pdp/* routes mounted (stash %s)\n", stashDir)

	// /piece/{pieceCid} HTTP retrieval is part of the public surface.
	retrieval.Routes(publicMux, eng.DB(), stashDir)
	fmt.Printf("  retrieval:/piece/{pieceCid} mounted (HTTP Range, ETag, immutable cache)\n")

	// Admin mux: /admin/* (test-tx, eth-key). Loopback-only by intent.
	adminMux := chi.NewRouter()
	var adminSender *message.SenderETH
	if chainDeps != nil {
		adminSender = chainDeps.SenderETH
	}
	admin.Routes(adminMux, eng.DB(), adminSender)
	fmt.Printf("  admin:    /admin/test-tx, /admin/eth-key mounted (loopback)\n")

	// Operator + client dashboard (Curio Core branded). Wired as the
	// fallthrough behind setupweb's first-run guard: while first-run
	// is incomplete every non-setup request still redirects to /setup;
	// once complete, requests fall into the dashboard.
	dashMux := chi.NewRouter()
	{
		payeeForDash := ""
		if p, err := ethkeys.LookupPDP(rootCtx, eng.DB()); err == nil {
			payeeForDash = p
		}
		dashCfg := dashboard.Config{
			Network:      *network,
			Version:      versionTag,
			PayeeAddress: payeeForDash,
			StashDir:     stashDir,
			DataDir:      *dataDir,
		}
		if chainDeps != nil {
			dashCfg.EthClient = chainDeps.EthClient
		}
		dashSrv, dErr := dashboard.NewServer(eng.DB(), dashCfg)
		if dErr != nil {
			_ = eng.Stop()
			return fmt.Errorf("dashboard.NewServer: %w", dErr)
		}
		dashSrv.Routes(dashMux)
		fmt.Printf("  dashboard: /, /wallets, /datasets, /rails, /tasks, /alerts mounted (Curio Core branded)\n")
	}
	setupHandler := setupweb.New(eng.DB())
	setupHandler.Inner = dashMux
	setupHandler.DisableFirstRunRedirect = true
	// Admin handler: /admin/* on adminMux, everything else (dashboard +
	// setup) on setupHandler. Reuse adminFallback so /admin/* doesn't
	// fall into the dashboard.
	adminHandler := adminFallback(adminMux, setupHandler)

	// Resolve admin bind: --admin-listen wins, else legacy --listen.
	adminBind := *adminListen
	if adminBind == "" {
		adminBind = *listenAddr
	}

	// Single-port back-compat: when --public-listen is empty, serve the
	// whole surface (public + admin) on the admin bind via the original
	// FallbackHandler, exactly as before the two-port split. This keeps
	// existing deploys (cc-smoke uses only --listen) working unchanged.
	//
	// publicMux + adminMux both register routes at their FULL paths
	// (/pdp/..., /piece/..., /admin/...), so we dispatch by prefix
	// rather than chi-Mount (which would double-prefix). combinedChi
	// routes /pdp|/piece -> publicMux, /admin -> adminMux, everything
	// else -> setupHandler.
	var servers *httpserve.Servers
	if *publicListen == "" {
		combined := combinedRouter(publicMux, adminMux, setupHandler)
		srv := &http.Server{
			Handler:           combined,
			ReadTimeout:       30 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
		}
		ln, lerr := net.Listen("tcp", adminBind)
		if lerr != nil {
			_ = eng.Stop()
			if lanternDaemon != nil {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = lanternDaemon.Stop(stopCtx)
				stopCancel()
			}
			return fmt.Errorf("http listen: %w", lerr)
		}
		fmt.Printf("  webui:    http://%s/ (single-port mode; pass --public-listen to split)\n", ln.Addr())
		serveErr := make(chan error, 1)
		go func() {
			e := srv.Serve(ln)
			if !errors.Is(e, http.ErrServerClosed) {
				serveErr <- e
				return
			}
			serveErr <- nil
		}()
		return waitAndShutdownSingle(rootCtx, srv, serveErr, eng, lanternDaemon)
	}

	// Two-port mode.
	var certCache autocert.Cache
	if *publicTLSDomain != "" {
		certCache = acmecache.New(eng.DB())
	}
	servers, err = httpserve.Build(httpserve.Config{
		AdminListen:           adminBind,
		PublicListen:          *publicListen,
		PublicTLSDomain:       *publicTLSDomain,
		ACMEHTTPChallengeAddr: *acmeHTTPAddr,
		ACMEDirectoryURL:      *acmeDirectoryURL,
		AdminHandler:          adminHandler,
		PublicHandler:         publicMux,
		Cache:                 certCache,
	})
	if err != nil {
		_ = eng.Stop()
		if lanternDaemon != nil {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = lanternDaemon.Stop(stopCtx)
			stopCancel()
		}
		return fmt.Errorf("httpserve.Build: %w", err)
	}
	fmt.Printf("  admin-ui: http://%s/ (loopback)\n", servers.AdminLn.Addr())
	if servers.TLSActive() {
		fmt.Printf("  public:   %s (autocert TLS for %s, ACME HTTP-01 on %s)\n", servers.PublicURL(), *publicTLSDomain, *acmeHTTPAddr)
	} else {
		fmt.Printf("  public:   %s (PLAINTEXT \u2014 dev mode, no --public-tls-domain)\n", servers.PublicURL())
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- servers.Run(rootCtx)
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
	// Cancel rootCtx so servers.Run() unwinds its listeners gracefully
	// (it owns Admin/Public/ACME Shutdown internally), then wait for it.
	cancelRoot()
	<-serveErr

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

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

// adminFallback routes /admin/* to adminMux and everything else to inner
// (the dashboard/setup handler). Mirrors pdpwire.FallbackHandler but for
// the admin port's narrower /admin/* prefix.
func adminFallback(adminMux http.Handler, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/admin" || strings.HasPrefix(p, "/admin/") {
			adminMux.ServeHTTP(w, r)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// combinedRouter dispatches by path prefix for single-port back-compat:
// /pdp + /piece -> publicMux, /admin -> adminMux, else -> inner. The
// muxes already hold full-path routes, so we must NOT chi-Mount them
// under a prefix (that would double-prefix to /pdp/pdp/...). Plain
// prefix dispatch preserves the pre-split routing exactly.
func combinedRouter(publicMux, adminMux, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/pdp" || strings.HasPrefix(p, "/pdp/") ||
			p == "/piece" || strings.HasPrefix(p, "/piece/"):
			publicMux.ServeHTTP(w, r)
		case p == "/admin" || strings.HasPrefix(p, "/admin/"):
			adminMux.ServeHTTP(w, r)
		default:
			inner.ServeHTTP(w, r)
		}
	})
}

// waitAndShutdownSingle handles the signal-wait + ordered teardown for
// single-port back-compat mode (one *http.Server).
func waitAndShutdownSingle(rootCtx context.Context, srv *http.Server, serveErr chan error, eng *engine.Engine, lanternDaemon *lantern.Daemon) error {
	fmt.Printf("\ncurio-core is running. Ctrl-C to stop.\n")
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
