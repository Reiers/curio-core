// cmd_doctor.go — curio-core doctor: read-only health + reconciliation
// report between local SQLite state and on-chain PDPVerifier / USDFC /
// FilecoinPay state.
//
// v0.1 is observe-only: prints what's in the DB, what's on chain,
// and where they diverge. No mutations. Repair flags land in a
// follow-up once we have a clearer set of expected failure modes.
//
// Inspired by filecoin-project/curio#889.
//
// Usage:
//
//	curio-core doctor [--data-dir <path>] [--network <n>] [--gateway <url>]
//	                  [--vm-bridge-rpc <url>]

package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	lanternbuild "github.com/Reiers/lantern/build"

	cethclient "github.com/Reiers/curio-core/internal/ethclient"
	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/wallet"
)

func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Network: mainnet | calibration")
	gateway := fs.String("gateway", "", "Lantern gateway URL (default per --network)")
	vmBridgeRPC := fs.String("vm-bridge-rpc", "", "VM bridge RPC URL (default: per-network Glif)")
	timeout := fs.Duration("timeout", 45*time.Second, "Total doctor probe timeout")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Printf("curio-core doctor: read-only health + reconciliation report\n")
	fmt.Printf("  data-dir: %s\n", *dataDir)
	fmt.Printf("  network:  %s\n\n", *network)

	// --- 1. SQLite state ---
	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	fmt.Println(divider("Local SQLite state"))
	if err := reportSQLiteState(ctx, db); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	// --- 2. Embedded Lantern + ethclient (boots a probe daemon) ---
	fmt.Println()
	fmt.Println(divider("Chain state via embedded Lantern"))

	eth, stop, err := bootLanternForRead(ctx, *dataDir, *network, *gateway, *vmBridgeRPC)
	if err != nil {
		return fmt.Errorf("doctor: %w", err)
	}
	defer stop()
	fmt.Printf("  lantern + ethclient ready (shared boot helper).\n")

	// --- 3. On-chain balance for each local wallet ---
	fmt.Println()
	fmt.Println(divider("Wallet balances on chain"))
	if err := reportWalletBalances(ctx, db, eth, *network); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	// --- 4. Recent message_sends_eth ---
	fmt.Println()
	fmt.Println(divider("Recent FEVM transactions (last 10)"))
	if err := reportRecentMessages(ctx, db); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	// --- 5. Payments readiness (USDFC prerequisite) ---
	fmt.Println()
	fmt.Println(divider("Payments readiness (USDFC)"))
	if err := reportPaymentsReadiness(ctx, db, eth, *network); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	// --- 6. PDP datasets summary ---
	fmt.Println()
	fmt.Println(divider("PDP datasets"))
	if err := reportPDPDatasets(ctx, db); err != nil {
		fmt.Printf("  WARN: %v\n", err)
	}

	fmt.Println()
	fmt.Println("Doctor report complete. No mutations performed (v0.1 is observe-only).")
	return nil
}

// --- reporters --------------------------------------------------------

func reportSQLiteState(ctx context.Context, db *harmonysqlite.DB) error {
	// Wallets
	wallets, err := wallet.List(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("  wallets:                       %d\n", len(wallets))

	// Streaming uploads
	var streamingTotal, streamingComplete int
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM pdp_piece_streaming_uploads`).Scan(&streamingTotal)
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM pdp_piece_streaming_uploads WHERE complete = 1`).Scan(&streamingComplete)
	fmt.Printf("  streaming uploads (total):     %d\n", streamingTotal)
	fmt.Printf("  streaming uploads (complete):  %d\n", streamingComplete)

	// Parked pieces
	var parked, parkedComplete int
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM parked_pieces`).Scan(&parked)
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM parked_pieces WHERE complete = 1`).Scan(&parkedComplete)
	fmt.Printf("  parked_pieces (total):         %d\n", parked)
	fmt.Printf("  parked_pieces (complete):      %d\n", parkedComplete)

	// Piecerefs
	var piecerefs int
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM pdp_piecerefs`).Scan(&piecerefs)
	fmt.Printf("  pdp_piecerefs:                 %d\n", piecerefs)

	// Datasets
	var datasets int
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM pdp_data_sets`).Scan(&datasets)
	fmt.Printf("  pdp_data_sets:                 %d\n", datasets)

	// Harmony tasks
	var tasks int
	_ = db.QueryRowI(ctx, `SELECT COUNT(*) FROM harmony_task`).Scan(&tasks)
	fmt.Printf("  harmony_task (queued/running): %d\n", tasks)

	return nil
}

func reportWalletBalances(ctx context.Context, db *harmonysqlite.DB, eth *cethclient.Client, network string) error {
	wallets, err := wallet.List(ctx, db)
	if err != nil {
		return err
	}
	if len(wallets) == 0 {
		fmt.Println("  (no wallets in eth_keys)")
		return nil
	}

	usdfcAddr, hasUSDFC := usdfcAddressFor(network)

	fmt.Printf("  %-44s  %-8s  %-22s  %s\n", "ADDRESS", "ROLE", "tFIL", "USDFC")
	for _, wlt := range wallets {
		addr := common.HexToAddress(wlt.Address)

		// tFIL balance
		fil, err := eth.BalanceAt(ctx, addr, nil)
		filStr := "?"
		if err == nil {
			filStr = formatBigWei(fil)
		}

		// USDFC balance via balanceOf(address) — selector 0x70a08231
		usdfcStr := "—"
		if hasUSDFC {
			balanceOf := append([]byte{0x70, 0xa0, 0x82, 0x31}, common.LeftPadBytes(addr.Bytes(), 32)...)
			out, err := eth.CallContract(ctx, ethCallMsg(usdfcAddr, balanceOf), nil)
			if err == nil && len(out) > 0 {
				v := new(big.Int).SetBytes(out)
				usdfcStr = formatBigWei(v)
			} else if err != nil {
				usdfcStr = "ERR"
			}
		}

		fmt.Printf("  %-44s  %-8s  %-22s  %s\n", wlt.Address, wlt.Role, filStr, usdfcStr)
	}
	return nil
}

// reportPaymentsReadiness checks the PDP wallet's USDFC balance and tells
// the operator, in plain language, whether the SP can create paid datasets.
//
// Datasets are paid in USDFC, not FIL. A wallet with FIL but 0 USDFC will
// sail through SP registration and then have CreateDataSet REVERT on-chain
// with FWSS InsufficientLockupFunds. This preflight surfaces that BEFORE
// the user wastes gas on a doomed tx. (curio-core#91)
func reportPaymentsReadiness(ctx context.Context, db *harmonysqlite.DB, eth *cethclient.Client, network string) error {
	wallets, err := wallet.List(ctx, db)
	if err != nil {
		return err
	}
	var pdpAddr string
	for _, w := range wallets {
		if w.Role == "pdp" {
			pdpAddr = w.Address
			break
		}
	}
	if pdpAddr == "" {
		fmt.Println("  no role=pdp wallet found — run setup first.")
		return nil
	}
	usdfcAddr, hasUSDFC := usdfcAddressFor(network)
	if !hasUSDFC {
		fmt.Printf("  USDFC address unknown for network %q — skipping.\n", network)
		return nil
	}
	addr := common.HexToAddress(pdpAddr)
	balanceOf := append([]byte{0x70, 0xa0, 0x82, 0x31}, common.LeftPadBytes(addr.Bytes(), 32)...)
	out, err := eth.CallContract(ctx, ethCallMsg(usdfcAddr, balanceOf), nil)
	if err != nil {
		return fmt.Errorf("read USDFC balance: %w", err)
	}
	bal := new(big.Int).SetBytes(out)

	fmt.Printf("  pdp wallet:                    %s\n", pdpAddr)
	fmt.Printf("  USDFC balance:                 %s\n", formatBigWei(bal))
	if bal.Sign() > 0 {
		fmt.Println("  status:                        ✓ has USDFC.")
		fmt.Println("  next:                          ensure operator approval is set:")
		fmt.Println("                                 curio-core demo prepare-client-payments --submit")
		return nil
	}
	// 0 USDFC: the blocking case. Be loud and actionable.
	fmt.Println("  status:                        ✗ NOT READY — 0 USDFC.")
	fmt.Println("  why:                           Datasets are paid in USDFC, not FIL. With 0 USDFC,")
	fmt.Println("                                 CreateDataSet will REVERT (FWSS InsufficientLockupFunds).")
	fmt.Println("  fix:                           Acquire USDFC for this wallet, then run")
	fmt.Println("                                 'curio-core demo prepare-client-payments --submit'.")
	fmt.Println("                                 USDFC is minted via a Secured Finance trove (min 200 USDFC")
	fmt.Println("                                 debt, >=110% FIL collateral) or received as a transfer.")
	return nil
}

func reportRecentMessages(ctx context.Context, db *harmonysqlite.DB) error {
	var rows []struct {
		FromAddress string `db:"from_address"`
		ToAddress   string `db:"to_address"`
		SendReason  string `db:"send_reason"`
		Nonce       int64  `db:"nonce"`
		SendSuccess bool   `db:"send_success"`
		SignedHash  string `db:"signed_hash"`
	}
	if err := db.SelectI(ctx, &rows, `
		SELECT from_address, to_address, send_reason, COALESCE(nonce,0) AS nonce, 
		       COALESCE(send_success,0) AS send_success, COALESCE(signed_hash,'') AS signed_hash
		FROM message_sends_eth
		ORDER BY send_task_id DESC
		LIMIT 10
	`); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("  (no FEVM transactions yet)")
		return nil
	}
	fmt.Printf("  %-44s  %-12s  %-6s  %s\n", "TO", "REASON", "OK?", "TX HASH")
	for _, r := range rows {
		hash := r.SignedHash
		if hash == "" {
			hash = "(pending)"
		}
		ok := "no"
		if r.SendSuccess {
			ok = "yes"
		}
		fmt.Printf("  %-44s  %-12s  %-6s  %s\n", r.ToAddress, truncate(r.SendReason, 12), ok, hash)
	}
	return nil
}

func reportPDPDatasets(ctx context.Context, db *harmonysqlite.DB) error {
	var rows []struct {
		ID       int64  `db:"id"`
		Service  string `db:"service"`
		InitOK   int64  `db:"init_ready"`
		CreateTx string `db:"create_message_hash"`
	}
	if err := db.SelectI(ctx, &rows, `
		SELECT id, service, init_ready, create_message_hash
		FROM pdp_data_sets
		ORDER BY id DESC
		LIMIT 20
	`); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Println("  (no datasets locally; SP has not created any yet)")
		return nil
	}
	fmt.Printf("  %-6s  %-12s  %-8s  %s\n", "ID", "SERVICE", "INIT-OK", "CREATE-TX")
	for _, r := range rows {
		ok := "no"
		if r.InitOK == 1 {
			ok = "yes"
		}
		fmt.Printf("  %-6d  %-12s  %-8s  %s\n", r.ID, truncate(r.Service, 12), ok, r.CreateTx)
	}
	return nil
}

// --- helpers ---------------------------------------------------------

func divider(title string) string {
	return fmt.Sprintf("=== %s %s", title, strings.Repeat("=", 70-len(title)))
}

func usdfcAddressFor(network string) (common.Address, bool) {
	switch network {
	case "calibration":
		return common.HexToAddress("0xb3042734b608a1B16e9e86B374A3f3e389B4cDf0"), true
	case "mainnet":
		return common.HexToAddress("0x80B98d3aa09ffff255c3ba4A241111Ff1262F045"), true
	}
	return common.Address{}, false
}

// formatBigWei renders an 18-decimal big.Int as a fixed-point string
// with 4 decimal places (truncating, not rounding).
func formatBigWei(v *big.Int) string {
	if v == nil {
		return "?"
	}
	if v.Sign() == 0 {
		return "0.0000"
	}
	// Convert to a string of base-10 digits.
	abs := new(big.Int).Abs(v)
	s := abs.String()
	if len(s) <= 18 {
		// Less than 1 unit. Pad with leading zeros for the decimal part.
		pad := 18 - len(s)
		dec := strings.Repeat("0", pad) + s
		// Truncate to 4 decimal places.
		if len(dec) > 4 {
			dec = dec[:4]
		}
		return "0." + dec
	}
	whole := s[:len(s)-18]
	dec := s[len(s)-18:]
	if len(dec) > 4 {
		dec = dec[:4]
	}
	return whole + "." + dec
}

// truncate caps s at n chars with a trailing ellipsis if needed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// ethCallMsg is a small adapter for eth.CallContract's go-ethereum
// CallMsg shape. Kept inline to avoid a separate package.
func ethCallMsg(to common.Address, data []byte) ethereum.CallMsg {
	return ethereum.CallMsg{To: &to, Data: data}
}
