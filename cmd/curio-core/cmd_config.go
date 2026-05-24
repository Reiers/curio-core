// cmd_config.go — headless first-run config CLI.
//
// Subcommands:
//
//	curio-core config show                       print current default-layer config
//	curio-core config set <field> <value>        set a single field (partial OK)
//	curio-core config status                     print completeness + missing fields
//
// All operations open the SQLite state DB directly; the daemon does
// NOT need to be running. After setting all required fields, restart
// the daemon (or boot it for the first time) and the /setup middleware
// will fall through to normal operation.
//
// Known fields (case-insensitive, multiple aliases per field):
//
//	pdp.miner-id  / miner_id / minerid    -> Pdp.MinerID
//	pdp.wallet    / wallet_address        -> Pdp.WalletAddress
//	pdp.market    / market_address        -> Pdp.MarketAddress
//
// Driven by curio-core#8 (Andy's 'we never need to run without a
// miner ID' invariant). The WebUI /setup flow keeps working in
// parallel; both paths write to the same SQLite row.

package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func cmdConfig(args []string) error {
	if len(args) == 0 {
		configUsage()
		return fmt.Errorf("subcommand required")
	}
	switch args[0] {
	case "show":
		return cmdConfigShow(args[1:])
	case "set":
		return cmdConfigSet(args[1:])
	case "status":
		return cmdConfigStatus(args[1:])
	case "-h", "--help", "help":
		configUsage()
		return nil
	default:
		configUsage()
		return fmt.Errorf("unknown config subcommand: %s", args[0])
	}
}

func configUsage() {
	fmt.Print(`curio-core config

  Headless first-run config for curio-core. Writes to the SQLite
  state DB's harmony_config table (default layer). After all required
  fields are set, 'curio-core run' boots without the /setup gate.

Subcommands:
  show                  Print the current default-layer config (TOML).
  set <field> <value>   Set one field. Partial; safe to call repeatedly.
  status                Print completeness + missing fields.

Known fields (case-insensitive):
  pdp.miner-id    f0xxx / 0xhex / t0xxx (the SP's on-chain miner ID)
  pdp.wallet      0xhex (the eth-address backing the SP's signer)
  pdp.market      0xhex (the FilOzone FWSS proxy this SP serves)

Common flags:
  --data-dir <path>     curio-core data directory (default: $HOME/.curio-core)
`)
}

// --- show ------------------------------------------------------------

func cmdConfigShow(args []string) error {
	fs := flag.NewFlagSet("config show", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	fs.Parse(args)

	ctx := context.Background()
	db, closeDB, err := openConfigDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	cfg, err := config.ReadDefaultLayer(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("[Pdp]\n")
	fmt.Printf("  MarketAddress = %q\n", cfg.Pdp.MarketAddress)
	fmt.Printf("  WalletAddress = %q\n", cfg.Pdp.WalletAddress)
	fmt.Printf("  MinerID       = %q\n", cfg.Pdp.MinerID)
	return nil
}

// --- set -------------------------------------------------------------

func cmdConfigSet(args []string) error {
	fs := flag.NewFlagSet("config set", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	fs.Parse(args)

	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: curio-core config set <field> <value>  (got %d positional args)", len(rest))
	}
	field, value := rest[0], rest[1]

	ctx := context.Background()
	db, closeDB, err := openConfigDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	if err := config.SetField(ctx, db, field, value); err != nil {
		return err
	}
	fmt.Printf("set %s = %q\n", field, value)

	// Print updated status so the operator sees how close they are
	// to a runnable config without a second invocation.
	st, err := config.Status(ctx, db)
	if err != nil {
		return nil // setField succeeded; status read is best-effort
	}
	if st.NeedsSetup {
		fmt.Printf("status: needs setup (missing: %v)\n", st.Missing)
	} else {
		fmt.Println("status: complete — 'curio-core run' will boot without /setup gate")
	}
	return nil
}

// --- status ----------------------------------------------------------

func cmdConfigStatus(args []string) error {
	fs := flag.NewFlagSet("config status", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	fs.Parse(args)

	ctx := context.Background()
	db, closeDB, err := openConfigDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	st, err := config.Status(ctx, db)
	if err != nil {
		return err
	}
	if st.NeedsSetup {
		fmt.Printf("needs setup: yes\n")
		fmt.Printf("missing fields: %v\n", st.Missing)
		return nil
	}
	fmt.Println("needs setup: no")
	fmt.Println("'curio-core run' will boot without /setup gate")
	return nil
}

// --- helpers ---------------------------------------------------------

// openConfigDB opens a *harmonysqlite.DB pointed at <data-dir>/state.sqlite.
// Used by every config subcommand. Returns a close func the caller
// must defer.
func openConfigDB(ctx context.Context, dataDir string) (*harmonysqlite.DB, func(), error) {
	dbPath := filepath.Join(dataDir, "state.sqlite")
	db, err := harmonysqlite.Open(harmonysqlite.Config{Path: dbPath})
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", dbPath, err)
	}
	return db, func() { _ = db.Close() }, nil
}
