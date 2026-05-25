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
//	curio-core wallet send [--asset fil|usdfc] <to> <amount>   broadcast value transfer
//
// list/new/import/export/role/delete operate on the DB directly; the
// daemon does NOT need to be running for those. After mutations the
// operator should restart `curio-core run` (or wait for the next
// eth_keys re-read) so the PDP task pipeline picks up the change.
//
// `send` is different — it REQUIRES the daemon to be running because
// it posts to /admin/test-tx, which dispatches through SenderETH and
// gets persisted in message_sends_eth + watched on-chain.

package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/wallet"
	"github.com/filecoin-project/curio/pdp/contract"
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
	case "send":
		return cmdWalletSend(rest)
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
  send [--asset fil|usdfc] <to> <amount>  Broadcast a value transfer (requires running daemon).

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

// --- send ------------------------------------------------------------

// selErc20Transfer is the 4-byte selector for ERC-20 transfer(address,uint256).
var selErc20Transfer = mustSelector("transfer(address,uint256)")

func cmdWalletSend(args []string) error {
	fs := flag.NewFlagSet("wallet send", flag.ExitOnError)
	asset := fs.String("asset", "fil", "asset to send (fil | usdfc)")
	daemon := fs.String("daemon", "http://127.0.0.1:14994", "daemon base URL (/admin/test-tx will be appended)")
	network := fs.String("network", "calibration", "network (calibration | mainnet) — used to resolve USDFC contract address")
	dryRun := fs.Bool("dry-run", false, "print the tx payload without submitting")
	fs.Parse(args)

	positional := fs.Args()
	if len(positional) != 2 {
		return fmt.Errorf("usage: curio-core wallet send [--asset fil|usdfc] [--daemon URL] [--network N] [--dry-run] <to> <amount>")
	}
	if !common.IsHexAddress(positional[0]) {
		return fmt.Errorf("not a valid 0x-prefixed address: %q", positional[0])
	}
	to := common.HexToAddress(positional[0])

	assetLower := strings.ToLower(*asset)
	switch assetLower {
	case "fil", "usdfc":
	default:
		return fmt.Errorf("--asset must be 'fil' or 'usdfc', got %q", *asset)
	}

	// Parse the amount as a decimal in the asset's display unit
	// (FIL or USDFC), convert to base units (attoFIL / USDFC base).
	// Both FIL and USDFC use 18 decimals on EVM rails, so the
	// conversion is identical: amount * 10^18.
	amountStr := positional[1]
	amountBase, err := parseDecimalTo18Decimals(amountStr)
	if err != nil {
		return fmt.Errorf("parse amount %q: %w", amountStr, err)
	}
	if amountBase.Sign() <= 0 {
		return fmt.Errorf("amount must be positive, got %s", amountStr)
	}

	ctx := context.Background()

	var txTo common.Address
	var txValue *big.Int
	var txData []byte

	switch assetLower {
	case "fil":
		// Native FIL transfer: msg.value = amount, data = nil.
		txTo = to
		txValue = amountBase
		txData = nil
	case "usdfc":
		// ERC-20 transfer: tx.to = USDFC contract, msg.value = 0,
		// data = transfer(to, amount).
		usdfcAddr, err := contract.USDFCAddressFor(contract.Network(*network))
		if err != nil {
			return fmt.Errorf("resolve USDFC address for network %q: %w", *network, err)
		}
		abiAddress, err := abi.NewType("address", "", nil)
		if err != nil {
			return err
		}
		abiUint256, err := abi.NewType("uint256", "", nil)
		if err != nil {
			return err
		}
		calldata, err := encodeCall(
			selErc20Transfer,
			abi.Arguments{{Type: abiAddress}, {Type: abiUint256}},
			to,
			amountBase,
		)
		if err != nil {
			return fmt.Errorf("encode transfer(to, amount): %w", err)
		}
		txTo = usdfcAddr
		txValue = big.NewInt(0)
		txData = calldata
	}

	fmt.Printf("wallet send:\n")
	fmt.Printf("  asset:   %s\n", strings.ToUpper(assetLower))
	fmt.Printf("  to:      %s\n", to.Hex())
	fmt.Printf("  amount:  %s %s (= %s base units)\n", amountStr, strings.ToUpper(assetLower), amountBase.String())
	if assetLower == "usdfc" {
		fmt.Printf("  via:     %s (USDFC on %s)\n", txTo.Hex(), *network)
	}

	if *dryRun {
		fmt.Printf("\n--dry-run set; not submitting. Calldata: 0x%x\n", txData)
		return nil
	}

	txHash, err := submitViaAdminTestTx(ctx, *daemon, txTo, txValue, txData)
	if err != nil {
		return fmt.Errorf("submit via daemon %s: %w (is the daemon running? try 'curio-core run')", *daemon, err)
	}
	fmt.Printf("  txHash:  %s\n", txHash)
	return nil
}

// parseDecimalTo18Decimals parses a decimal string (e.g. "1.5" or "0.001")
// and returns the value in base units assuming 18 decimal places.
// Both FIL and USDFC use 18 decimals on EVM rails.
func parseDecimalTo18Decimals(s string) (*big.Int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	} else if s[0] == '+' {
		s = s[1:]
	}

	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if intPart == "" {
		intPart = "0"
	}

	if len(fracPart) > 18 {
		return nil, fmt.Errorf("more than 18 decimal places")
	}
	// Pad fractional part to 18 digits.
	fracPadded := fracPart + strings.Repeat("0", 18-len(fracPart))

	combined := intPart + fracPadded
	// Strip a leading zero pad so big.Int sees clean decimal.
	combined = strings.TrimLeft(combined, "0")
	if combined == "" {
		combined = "0"
	}

	n, ok := new(big.Int).SetString(combined, 10)
	if !ok {
		return nil, fmt.Errorf("not a valid decimal: %q", s)
	}
	if neg {
		n = n.Neg(n)
	}
	return n, nil
}
