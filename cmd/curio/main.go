package main

import (
	"bufio"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/doctor"
	importer "github.com/Reiers/curio-core/internal/import"
	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/node"
	"github.com/Reiers/curio-core/internal/snapshot"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/wallet"
)

func main() {
	logger := logging.New()
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		if err := cmdInit(os.Args[2:], logger); err != nil {
			logger.Errorf("init failed: %v", err)
			os.Exit(1)
		}
	case "sync":
		if err := cmdSync(os.Args[2:], logger); err != nil {
			logger.Errorf("sync failed: %v", err)
			os.Exit(1)
		}
	case "snapshot":
		if err := cmdSnapshot(os.Args[2:], logger); err != nil {
			logger.Errorf("snapshot failed: %v", err)
			os.Exit(1)
		}
	case "status":
		if err := cmdStatus(os.Args[2:], logger); err != nil {
			logger.Errorf("status failed: %v", err)
			os.Exit(1)
		}
	case "doctor":
		if err := cmdDoctor(os.Args[2:], logger); err != nil {
			logger.Errorf("doctor failed: %v", err)
			os.Exit(1)
		}
	case "chain":
		if err := cmdChain(os.Args[2:], logger); err != nil {
			logger.Errorf("chain failed: %v", err)
			os.Exit(1)
		}
	case "wallet":
		if err := cmdWallet(os.Args[2:], logger); err != nil {
			logger.Errorf("wallet failed: %v", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Curio Core (alpha)")
	fmt.Println("Commands:")
	fmt.Println("  curio init")
	fmt.Println("  curio sync [--explain]")
	fmt.Println("  curio doctor [--explain]")
	fmt.Println("  curio snapshot download|import|cleanup")
	fmt.Println("  curio status")
	fmt.Println("  curio chain msg --decode <hex|base64> [--explain]")
	fmt.Println("  curio chain coverage-report")
	fmt.Println("  curio wallet new|list|show|export|import|resolve|sign|verify")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  curio doctor --explain")
	fmt.Println("  curio chain msg --decode 0x68656c6c6f")
	fmt.Println("  curio wallet new --name worker-a --type secp --explain")
}

func defaultHome() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".curio")
}

func cmdInit(args []string, log *logging.Logger) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultHome(), "curio home")
	network := fs.String("network", "mainnet", "mainnet|calibnet")
	force := fs.Bool("force", false, "overwrite existing config")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := config.Default(*dataDir)
	cfg.Network = *network
	if err := config.InitDirs(cfg); err != nil {
		return err
	}
	if err := config.Save(cfg, *force); err != nil {
		return err
	}
	log.Infof("initialized at %s", cfg.HomeDir)
	return nil
}

func cmdSync(args []string, log *logging.Logger) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultHome(), "curio home")
	networkFlag := fs.String("network", "", "mainnet|calibnet")
	modeFlag := fs.String("mode", "", "fast|manual|empty")
	snapshotFileFlag := fs.String("snapshot-file", "", "path to snapshot file")
	snapshotURL := fs.String("snapshot-url", "", "override snapshot URL")
	keepSnapshot := fs.Bool("keep-snapshot", false, "keep snapshot after import")
	yes := fs.Bool("yes", false, "non-interactive")
	explain := fs.Bool("explain", false, "show staged explanation before executing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadOrDefault(*dataDir)
	if err != nil {
		return err
	}
	if err := config.InitDirs(cfg); err != nil {
		return err
	}

	network := cfg.Network
	mode := "fast"
	snapshotFile := *snapshotFileFlag
	if *explain {
		fmt.Println("[explain] sync stages: configure -> snapshot(download/verify) -> import -> start skeleton -> cleanup")
	}
	if *networkFlag != "" {
		network = *networkFlag
	}
	if *modeFlag != "" {
		mode = *modeFlag
	}

	if !*yes {
		network = askChoice("Network", []string{"mainnet", "calibnet"}, network)
		mode = askChoice("Mode", []string{"fast", "manual", "empty"}, mode)
		cfg.DataDir = askInput("Storage path", cfg.DataDir)
		cfg.SnapshotKeep = askYesNo("Keep snapshot after import?", false)
	} else {
		cfg.SnapshotKeep = *keepSnapshot
	}

	cfg.Network = network
	cfg.Mode = mode
	if *snapshotURL != "" {
		cfg.SnapshotURLOverride = *snapshotURL
	}
	if err := config.Save(cfg, true); err != nil {
		return err
	}

	st := status.NewStore(cfg.StatusFile)
	if err := st.Set("starting", 0, "sync wizard started"); err != nil {
		return err
	}

	switch mode {
	case "fast":
		targetDir := filepath.Join(cfg.SnapshotDir, network)
		dl, err := snapshot.Download(log, st, network, cfg.ResolveSnapshotURL(), targetDir, 5)
		if err != nil {
			return err
		}
		if err := snapshot.Verify(log, st, dl); err != nil {
			return err
		}
		if err := importer.ImportFile(log, st, dl, cfg.NetworkDataDir()); err != nil {
			return err
		}
		if err := node.StartSkeleton(log, st, cfg); err != nil {
			return err
		}
		if !cfg.SnapshotKeep {
			if err := snapshot.CleanupFile(log, st, dl); err != nil {
				return err
			}
		}
	case "manual":
		if snapshotFile == "" && !*yes {
			snapshotFile = askInput("Snapshot file path", "")
		}
		if snapshotFile == "" {
			return errors.New("--snapshot-file is required in manual mode")
		}
		if err := snapshot.Verify(log, st, snapshotFile); err != nil {
			return err
		}
		if err := importer.ImportFile(log, st, snapshotFile, cfg.NetworkDataDir()); err != nil {
			return err
		}
		if err := node.StartSkeleton(log, st, cfg); err != nil {
			return err
		}
		if !cfg.SnapshotKeep {
			_ = snapshot.CleanupFile(log, st, snapshotFile)
		}
	case "empty":
		if err := node.StartSkeleton(log, st, cfg); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}

	return st.Set("complete", 100, "sync flow complete")
}

func cmdSnapshot(args []string, log *logging.Logger) error {
	if len(args) == 0 {
		return errors.New("usage: curio snapshot <download|import|cleanup>")
	}
	sub := args[0]
	subArgs := args[1:]

	home := defaultHome()
	cfg, _ := config.LoadOrDefault(home)
	st := status.NewStore(cfg.StatusFile)

	switch sub {
	case "download":
		fs := flag.NewFlagSet("snapshot download", flag.ContinueOnError)
		network := fs.String("network", cfg.Network, "mainnet|calibnet")
		url := fs.String("snapshot-url", cfg.ResolveSnapshotURL(), "snapshot URL")
		outDir := fs.String("output-dir", "", "output dir")
		conc := fs.Int("concurrency", 5, "aria2c concurrency")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		d := *outDir
		if d == "" {
			d = filepath.Join(cfg.SnapshotDir, *network)
		}
		_, err := snapshot.Download(log, st, *network, *url, d, *conc)
		return err
	case "import":
		fs := flag.NewFlagSet("snapshot import", flag.ContinueOnError)
		network := fs.String("network", cfg.Network, "mainnet|calibnet")
		file := fs.String("file", "", "snapshot file")
		keep := fs.Bool("keep-snapshot", false, "keep snapshot")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *file == "" {
			return errors.New("--file is required")
		}
		if err := snapshot.Verify(log, st, *file); err != nil {
			return err
		}
		cfg.Network = *network
		if err := importer.ImportFile(log, st, *file, cfg.NetworkDataDir()); err != nil {
			return err
		}
		if !*keep {
			return snapshot.CleanupFile(log, st, *file)
		}
		return nil
	case "cleanup":
		fs := flag.NewFlagSet("snapshot cleanup", flag.ContinueOnError)
		network := fs.String("network", cfg.Network, "mainnet|calibnet")
		all := fs.Bool("all", false, "remove all files")
		file := fs.String("file", "", "remove specific file")
		yes := fs.Bool("yes", false, "skip confirmation")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		target := filepath.Join(cfg.SnapshotDir, *network)
		if !*yes {
			fmt.Printf("cleanup snapshots in %s? [y/N]: ", target)
			if !askYes() {
				return nil
			}
		}
		if *file != "" {
			return snapshot.CleanupFile(log, st, *file)
		}
		if *all {
			return snapshot.CleanupDir(log, st, target)
		}
		return errors.New("use --all or --file")
	default:
		return fmt.Errorf("unknown snapshot subcommand: %s", sub)
	}
}

func cmdStatus(args []string, log *logging.Logger) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "json output")
	watch := fs.Bool("watch", false, "watch mode")
	dataDir := fs.String("data-dir", defaultHome(), "curio home")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, _ := config.LoadOrDefault(*dataDir)
	st := status.NewStore(cfg.StatusFile)

	printOnce := func() error {
		s, err := st.Read()
		if err != nil {
			return err
		}
		if *jsonOut {
			fmt.Println(s.JSON())
			return nil
		}
		fmt.Printf("stage=%s progress=%d%% updated=%s message=%s\n", s.Stage, s.Progress, s.UpdatedAt.Format(time.RFC3339), s.Message)
		return nil
	}
	if !*watch {
		return printOnce()
	}
	for {
		if err := printOnce(); err != nil {
			return err
		}
		time.Sleep(time.Second)
	}
}

func cmdDoctor(args []string, log *logging.Logger) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultHome(), "curio home")
	explain := fs.Bool("explain", false, "explain each check and remediation")
	jsonOut := fs.Bool("json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, _ := config.LoadOrDefault(*dataDir)
	checks, err := doctor.Run(cfg.HomeDir, cfg.DataDir)
	if err != nil {
		return err
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(checks, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println("Doctor report")
		for _, c := range checks {
			state := "OK"
			if !c.OK {
				state = "FAIL"
			}
			fmt.Printf(" - [%s] %s: %s\n", state, c.Name, c.Message)
			if !c.OK && c.Fix != "" {
				fmt.Printf("   fix: %s\n", c.Fix)
			}
			if *explain {
				fmt.Printf("   explain: check ensures %s is healthy before sync/import workflows.\n", c.Name)
			}
		}
	}
	for _, c := range checks {
		if !c.OK {
			return errors.New("doctor detected failures")
		}
	}
	log.Infof("doctor checks passed")
	return nil
}

func cmdChain(args []string, _ *logging.Logger) error {
	if len(args) == 0 {
		return errors.New("usage: curio chain <msg|coverage-report>")
	}
	switch args[0] {
	case "msg":
		fs := flag.NewFlagSet("chain msg", flag.ContinueOnError)
		decode := fs.String("decode", "", "decode hex/base64 message bytes")
		explain := fs.Bool("explain", false, "explain decode logic")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *decode == "" {
			return errors.New("use --decode <hex|base64>")
		}
		decoded, format, err := decodeMessage(*decode)
		if err != nil {
			return err
		}
		fmt.Printf("Decoded (%s): %q\n", format, string(decoded))
		if *explain {
			fmt.Println("[explain] attempts hex decode first (0x optional), then base64 fallback")
		}
		return nil
	case "coverage-report":
		fmt.Println("Chain coverage report (alpha scaffold)")
		fmt.Println(" - message decode: available")
		fmt.Println(" - actor map: TODO")
		fmt.Println(" - tipset drilldown: TODO")
		fmt.Println(" - parity checks: TODO")
		return nil
	default:
		return fmt.Errorf("unknown chain subcommand: %s", args[0])
	}
}

func cmdWallet(args []string, _ *logging.Logger) error {
	if len(args) == 0 {
		return errors.New("usage: curio wallet <new|list|show|export|import|resolve|sign|verify>")
	}
	home := defaultHome()
	sub := args[0]
	subArgs := args[1:]

	loadWithPrompt := func(op string) (*wallet.Store, string, error) {
		pass := askInput(fmt.Sprintf("Wallet password for %s", op), "")
		st, err := wallet.Load(home, pass)
		return st, pass, err
	}

	switch sub {
	case "new":
		fs := flag.NewFlagSet("wallet new", flag.ContinueOnError)
		name := fs.String("name", "", "wallet name")
		keyType := fs.String("type", "secp", "secp|bls|delegated")
		explain := fs.Bool("explain", false, "explain alpha key generation")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *name == "" {
			return errors.New("--name is required")
		}
		st, pass, err := loadWithPrompt("new")
		if err != nil {
			return err
		}
		e, err := st.Add(*name, wallet.KeyType(*keyType))
		if err != nil {
			return err
		}
		if err := wallet.Save(home, pass, st); err != nil {
			return err
		}
		fmt.Printf("[1/2] wallet record created: %s\n", e.Name)
		fmt.Printf("[2/2] address: %s (type=%s)\n", e.Address, e.KeyType)
		if *explain {
			fmt.Println("[explain] alpha mode uses placeholder key material and AES-GCM encrypted local storage")
		}
		return nil
	case "list":
		st, _, err := loadWithPrompt("list")
		if err != nil {
			return err
		}
		if len(st.Entries) == 0 {
			fmt.Println("No wallets found")
			return nil
		}
		for _, e := range st.Entries {
			fmt.Printf("- %s\t%s\t(%s)\n", e.Name, e.Address, e.KeyType)
		}
		return nil
	case "show":
		fs := flag.NewFlagSet("wallet show", flag.ContinueOnError)
		q := fs.String("wallet", "", "wallet name or address")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *q == "" {
			return errors.New("--wallet is required")
		}
		st, _, err := loadWithPrompt("show")
		if err != nil {
			return err
		}
		e, err := st.FindByNameOrAddress(*q)
		if err != nil {
			return err
		}
		fmt.Printf("name=%s address=%s type=%s created=%s\n", e.Name, e.Address, e.KeyType, e.CreatedAt.Format(time.RFC3339))
		return nil
	case "export":
		fmt.Println("wallet export scaffold: TODO (format selection + redaction safeguards)")
		return nil
	case "import":
		fs := flag.NewFlagSet("wallet import", flag.ContinueOnError)
		name := fs.String("name", "", "wallet name")
		keyType := fs.String("type", "secp", "secp|bls|delegated")
		priv := fs.String("private-key", "", "private key payload (alpha placeholder accepted)")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *name == "" || *priv == "" {
			return errors.New("--name and --private-key are required")
		}
		st, pass, err := loadWithPrompt("import")
		if err != nil {
			return err
		}
		e, err := st.Add(*name, wallet.KeyType(*keyType))
		if err != nil {
			return err
		}
		e.PrivateKey = *priv
		for i := range st.Entries {
			if st.Entries[i].Name == e.Name {
				st.Entries[i] = e
			}
		}
		if err := wallet.Save(home, pass, st); err != nil {
			return err
		}
		fmt.Printf("Imported wallet %s (%s)\n", e.Name, e.Address)
		return nil
	case "resolve":
		fs := flag.NewFlagSet("wallet resolve", flag.ContinueOnError)
		addr := fs.String("address", "", "address to resolve")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *addr == "" {
			return errors.New("--address is required")
		}
		if _, _, err := loadWithPrompt("resolve"); err != nil {
			return err
		}
		if strings.HasPrefix(*addr, "f2") {
			fmt.Println("TODO: f2 actor lookup requires live chain index integration")
			return nil
		}
		fmt.Printf("Address does not require on-chain resolve: %s\n", *addr)
		return nil
	case "sign":
		fs := flag.NewFlagSet("wallet sign", flag.ContinueOnError)
		q := fs.String("wallet", "", "wallet name or address")
		msg := fs.String("message", "", "message to sign")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *q == "" || *msg == "" {
			return errors.New("--wallet and --message are required")
		}
		st, _, err := loadWithPrompt("sign")
		if err != nil {
			return err
		}
		e, err := st.FindByNameOrAddress(*q)
		if err != nil {
			return err
		}
		sig := base64.StdEncoding.EncodeToString([]byte("sig:" + e.Address + ":" + *msg))
		fmt.Printf("Signature (alpha): %s\n", sig)
		return nil
	case "verify":
		fs := flag.NewFlagSet("wallet verify", flag.ContinueOnError)
		sig := fs.String("signature", "", "signature to verify")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		if *sig == "" {
			return errors.New("--signature is required")
		}
		_, err := base64.StdEncoding.DecodeString(*sig)
		if err != nil {
			fmt.Println("verify: invalid signature encoding")
			return nil
		}
		fmt.Println("verify: signature format accepted (alpha stub)")
		return nil
	default:
		return fmt.Errorf("unknown wallet subcommand: %s", sub)
	}
}

func decodeMessage(v string) ([]byte, string, error) {
	trim := strings.TrimSpace(v)
	trim = strings.TrimPrefix(trim, "0x")
	if b, err := hex.DecodeString(trim); err == nil {
		return b, "hex", nil
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, "", errors.New("decode failed: provide valid hex (0x...) or base64")
	}
	return b, "base64", nil
}

func askChoice(name string, opts []string, def string) string {
	fmt.Printf("%s [%s] (default %s): ", name, strings.Join(opts, "/"), def)
	r := bufio.NewReader(os.Stdin)
	v, _ := r.ReadString('\n')
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	for _, o := range opts {
		if v == o {
			return v
		}
	}
	return def
}

func askInput(name, def string) string {
	fmt.Printf("%s (default %s): ", name, def)
	r := bufio.NewReader(os.Stdin)
	v, _ := r.ReadString('\n')
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func askYesNo(prompt string, def bool) bool {
	defS := "y/N"
	if def {
		defS = "Y/n"
	}
	fmt.Printf("%s [%s]: ", prompt, defS)
	return askYesWithDefault(def)
}

func askYesWithDefault(def bool) bool {
	r := bufio.NewReader(os.Stdin)
	v, _ := r.ReadString('\n')
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return def
	}
	return v == "y" || v == "yes"
}

func askYes() bool { return askYesWithDefault(false) }
