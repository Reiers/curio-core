// cmd_wallet.go — operator wallet management on top of internal/wallet.
//
// Subcommands:
//
//	curio-core wallet list                            list all eth_keys rows
//	curio-core wallet new [--role <r>]                generate a fresh key
//	curio-core wallet import <0xhex> [--role <r>]     import an existing key
//	curio-core wallet export <addr> [--confirm]       print private key (DANGEROUS)
//	curio-core wallet role <addr> <new-role>          change a key's role
//	curio-core wallet delete <addr> [--yes]           remove a key
//
// All operations open the DB directly; the daemon does NOT need to be
// running. After mutations the operator should restart `curio-core run`
// (or wait for the next eth_keys re-read) so the PDP task pipeline
// picks up the change.

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/wallet"
)

func cmdWallet(args []string) error {
	if len(args) == 0 {
		walletUsage()
		return fmt.Errorf("subcommand required")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return cmdWalletList(rest)
	case "new":
		return cmdWalletNew(rest)
	case "import":
		return cmdWalletImport(rest)
	case "export":
		return cmdWalletExport(rest)
	case "role":
		return cmdWalletRole(rest)
	case "delete", "rm":
		return cmdWalletDelete(rest)
	case "-h", "--help", "help":
		walletUsage()
		return nil
	default:
		walletUsage()
		return fmt.Errorf("unknown wallet subcommand: %s", sub)
	}
}

func walletUsage() {
	fmt.Fprint(os.Stderr, `curio-core wallet

  Operator wallet management on top of the eth_keys SQLite table.

Subcommands:
  list                                List all wallets.
  new [--role <r>]                    Generate a fresh secp256k1 key. Default role: pdp.
  import [--role <r>] <0xhex>         Import an existing private key.
  export [--confirm] <addr>           Print the private key (DANGEROUS, requires --confirm).
  role <addr> <new-role>              Change a wallet's role.
  delete [--yes] <addr>               Remove a wallet (requires --yes).

Note: standard Go flag parsing — all --flags must come BEFORE positional
args. e.g.  curio-core wallet delete --yes --data-dir /path 0xabc...

Common flags:
  --data-dir <path>                   curio-core data directory (default: ~/.curio-core)

Notes:
  - The PDP task pipeline reads role='pdp' wallets. Removing the last
    pdp row is refused; use 'role' to demote it first.
  - export prints raw 32-byte hex. Treat as sensitive material.
  - After mutations, restart 'curio-core run' so the PDP task pipeline
    picks up the change.
`)
}

// --- list ------------------------------------------------------------

func cmdWalletList(args []string) error {
	fs := flag.NewFlagSet("wallet list", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	fs.Parse(args)

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	rows, err := wallet.List(ctx, db)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("No wallets. Run 'curio-core wallet new' or 'curio-core run' to bootstrap one.")
		return nil
	}
	fmt.Printf("%-44s  %s\n", "ADDRESS", "ROLE")
	for _, r := range rows {
		fmt.Printf("%-44s  %s\n", r.Address, r.Role)
	}
	return nil
}

// --- new -------------------------------------------------------------

func cmdWalletNew(args []string) error {
	fs := flag.NewFlagSet("wallet new", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	role := fs.String("role", "pdp", "Role for the new wallet (pdp | backup | operator | <custom>)")
	fs.Parse(args)

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	addr, err := wallet.New(ctx, db, *role)
	if err != nil {
		return err
	}
	fmt.Printf("Created wallet:\n")
	fmt.Printf("  address: %s\n", addr.Hex())
	fmt.Printf("  role:    %s\n", *role)
	fmt.Printf("\nFund the address from a faucet or transfer before relying on it.\n")
	if *role == "pdp" {
		fmt.Printf("Restart 'curio-core run' so the PDP task pipeline picks up the new wallet.\n")
	}
	return nil
}

// --- import ----------------------------------------------------------

func cmdWalletImport(args []string) error {
	fs := flag.NewFlagSet("wallet import", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	role := fs.String("role", "pdp", "Role for the imported wallet")
	keyFlag := fs.String("key", "", "Hex-encoded private key (0x-prefixed or bare). If not set, read from positional arg.")
	fs.Parse(args)

	keyHex := *keyFlag
	if keyHex == "" {
		positional := fs.Args()
		if len(positional) == 0 {
			return fmt.Errorf("provide private key as positional arg or via --key")
		}
		keyHex = positional[0]
	}

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	addr, err := wallet.Import(ctx, db, keyHex, *role)
	if err != nil {
		return err
	}
	fmt.Printf("Imported wallet:\n")
	fmt.Printf("  address: %s\n", addr.Hex())
	fmt.Printf("  role:    %s\n", *role)
	if *role == "pdp" {
		fmt.Printf("\nRestart 'curio-core run' so the PDP task pipeline picks up the new wallet.\n")
	}
	return nil
}

// --- export ----------------------------------------------------------

func cmdWalletExport(args []string) error {
	fs := flag.NewFlagSet("wallet export", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	confirm := fs.Bool("confirm", false, "Required: confirms the operator understands the private key will be printed in plaintext")
	fs.Parse(args)

	if !*confirm {
		return fmt.Errorf("export requires --confirm (prints the private key in plaintext; back up to a secure location)")
	}
	positional := fs.Args()
	if len(positional) == 0 {
		return fmt.Errorf("provide the address as a positional arg (e.g. 0xabc...)")
	}
	if !common.IsHexAddress(positional[0]) {
		return fmt.Errorf("not a valid 0x-prefixed address: %q", positional[0])
	}
	addr := common.HexToAddress(positional[0])

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	priv, err := wallet.Export(ctx, db, addr)
	if err != nil {
		return err
	}
	fmt.Printf("address:     %s\n", addr.Hex())
	fmt.Printf("private_key: 0x%s\n", hex.EncodeToString(priv))
	fmt.Printf("\nStore this somewhere safe. Anyone with this key can spend the wallet's funds.\n")
	return nil
}

// --- role ------------------------------------------------------------

func cmdWalletRole(args []string) error {
	fs := flag.NewFlagSet("wallet role", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	fs.Parse(args)

	positional := fs.Args()
	if len(positional) != 2 {
		return fmt.Errorf("usage: curio-core wallet role <address> <new-role>")
	}
	if !common.IsHexAddress(positional[0]) {
		return fmt.Errorf("not a valid 0x-prefixed address: %q", positional[0])
	}
	addr := common.HexToAddress(positional[0])
	newRole := positional[1]

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	if err := wallet.SetRole(ctx, db, addr, newRole); err != nil {
		return err
	}
	fmt.Printf("Updated %s -> role=%s\n", addr.Hex(), newRole)
	return nil
}

// --- delete ----------------------------------------------------------

func cmdWalletDelete(args []string) error {
	fs := flag.NewFlagSet("wallet delete", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	yes := fs.Bool("yes", false, "Required: confirms the deletion")
	fs.Parse(args)

	positional := fs.Args()
	if len(positional) != 1 {
		return fmt.Errorf("usage: curio-core wallet delete <address> --yes")
	}
	if !*yes {
		return fmt.Errorf("delete requires --yes (this is permanent; export the key first if you need a backup)")
	}
	if !common.IsHexAddress(positional[0]) {
		return fmt.Errorf("not a valid 0x-prefixed address: %q", positional[0])
	}
	addr := common.HexToAddress(positional[0])

	ctx := context.Background()
	db, close, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer close()

	removed, err := wallet.Delete(ctx, db, addr)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Printf("No wallet found at %s; nothing to delete.\n", addr.Hex())
		return nil
	}
	fmt.Printf("Deleted %s.\n", addr.Hex())
	return nil
}

// --- helpers ---------------------------------------------------------

// openWalletDB opens the curio-core SQLite state DB for read+write
// wallet operations. The caller MUST call the returned close func to
// release the DB file lock (so the daemon can pick it up afterward).
func openWalletDB(ctx context.Context, dataDir string) (*harmonysqlite.DB, func(), error) {
	dbPath := filepath.Join(dataDir, "state.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil, fmt.Errorf("state.sqlite not found at %s (run 'curio-core run' once to bootstrap, or pass --data-dir)", dbPath)
	}
	db, err := harmonysqlite.New(ctx, harmonysqlite.Config{
		Path:        dbPath,
		WALMode:     true,
		ForeignKeys: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open state.sqlite: %w", err)
	}
	return db, func() { _ = db.Close() }, nil
}

// Keep strings.* referenced to silence lint when no current call uses it.
var _ = strings.TrimSpace
