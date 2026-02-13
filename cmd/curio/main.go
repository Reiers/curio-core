package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Reiers/curio-core/internal/config"
	importer "github.com/Reiers/curio-core/internal/import"
	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/node"
	"github.com/Reiers/curio-core/internal/snapshot"
	"github.com/Reiers/curio-core/internal/status"
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
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("Curio Core (alpha)")
	fmt.Println("Commands:")
	fmt.Println("  curio init")
	fmt.Println("  curio sync")
	fmt.Println("  curio snapshot download")
	fmt.Println("  curio snapshot import")
	fmt.Println("  curio snapshot cleanup")
	fmt.Println("  curio status")
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
