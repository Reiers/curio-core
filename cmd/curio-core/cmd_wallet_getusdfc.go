package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Reiers/curio-core/internal/usdfcacquire"
	"github.com/Reiers/curio-core/internal/wallet"
)

// Known source chains + their canonical (native) USDC. The SP brings USDC on
// one of these and bridges it to USDFC on Filecoin via Squid. RPCs come from
// env (operators already have Alchemy/Infura keys): CURIO_RPC_<CHAIN>.
type srcChainDef struct {
	name     string
	chainID  int64
	usdc     string
	usdcDecs int
	rpcEnv   string
}

var srcChains = map[string]srcChainDef{
	"ethereum": {"ethereum", 1, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", 6, "CURIO_RPC_ETHEREUM"},
	"base":     {"base", 8453, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", 6, "CURIO_RPC_BASE"},
	"arbitrum": {"arbitrum", 42161, "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", 6, "CURIO_RPC_ARBITRUM"},
	"optimism": {"optimism", 10, "0x0b2C639c533813f4Aa9D7837CAf62653d097Ff85", 6, "CURIO_RPC_OPTIMISM"},
	"polygon":  {"polygon", 137, "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", 6, "CURIO_RPC_POLYGON"},
}

// integratorID resolves the Squid integrator id from env, then the vault file.
func integratorID() string {
	if v := strings.TrimSpace(os.Getenv("CURIO_SQUID_INTEGRATOR_ID")); v != "" {
		return v
	}
	// Best-effort vault read: ~/.openclaw/workspace/.vault/squid.md "id: <...>"
	home, _ := os.UserHomeDir()
	for _, p := range []string{home + "/.openclaw/workspace/.vault/squid.md"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "id:") {
				return strings.TrimSpace(line[3:])
			}
		}
	}
	return ""
}

func cmdWalletGetUSDFC(args []string) error {
	fs := flag.NewFlagSet("wallet get-usdfc", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	from := fs.String("from-chain", "base", "Source chain holding USDC: ethereum|base|arbitrum|optimism|polygon")
	amount := fs.String("amount", "", "USDFC target amount in whole tokens (e.g. 3). Bridges the equivalent USDC.")
	slippage := fs.Float64("slippage", 1.5, "Max slippage percent")
	submit := fs.Bool("submit", false, "Broadcast the source-chain tx (spends real funds). Omit for a quote-only dry-run.")
	fs.Parse(args)

	if *amount == "" {
		return fmt.Errorf("provide --amount (whole USDFC, e.g. --amount 3)")
	}
	sc, ok := srcChains[strings.ToLower(*from)]
	if !ok {
		return fmt.Errorf("unknown --from-chain %q (ethereum|base|arbitrum|optimism|polygon)", *from)
	}

	id := integratorID()
	client := usdfcacquire.NewClient(id, "")
	if !client.HasIntegratorID() {
		return fmt.Errorf(
			"no Squid integrator id.\n  Apply (self-serve, emailed back): https://squidrouter.typeform.com/integrator-id\n  Then: export CURIO_SQUID_INTEGRATOR_ID=<id>  (or put 'id: <id>' in .vault/squid.md)")
	}

	ctx := context.Background()
	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	// Resolve the pdp wallet = both the source sender AND the Filecoin
	// receiver (same secp256k1 key works on every EVM chain).
	wallets, err := wallet.List(ctx, db)
	if err != nil {
		return err
	}
	var pdp string
	for _, w := range wallets {
		if w.Role == "pdp" {
			pdp = w.Address
			break
		}
	}
	if pdp == "" {
		return fmt.Errorf("no role=pdp wallet; run setup first")
	}

	// USDFC has 18 decimals; we size the source USDC (6 decimals) ~1:1 to the
	// USDFC target (Squid returns the precise output). fromAmount is USDC.
	whole, okAmt := new(big.Int).SetString(strings.TrimSpace(*amount), 10)
	if !okAmt || whole.Sign() <= 0 {
		return fmt.Errorf("--amount must be a positive whole number of USDFC")
	}
	usdcUnits := new(big.Int).Mul(whole, pow10(sc.usdcDecs))

	fmt.Printf("Acquire USDFC via Squid (headless, no browser):\n")
	fmt.Printf("  source:      %s USDC on %s (chain %d)\n", whole.String(), sc.name, sc.chainID)
	fmt.Printf("  destination: USDFC on Filecoin (chain 314) -> %s\n", pdp)

	resp, err := client.Route(ctx, usdfcacquire.RouteParams{
		FromChain:   fmt.Sprintf("%d", sc.chainID),
		FromToken:   sc.usdc,
		FromAmount:  usdcUnits.String(),
		FromAddress: pdp,
		ToAddress:   pdp,
		Slippage:    *slippage,
		QuoteOnly:   !*submit,
	})
	if err != nil {
		return err
	}
	est := resp.Route.Estimate
	fmt.Printf("\n  quote:\n")
	fmt.Printf("    you receive (est):  %s USDFC (min %s)\n", formatToken(est.ToAmount, 18), formatToken(est.ToAmountMin, 18))
	fmt.Printf("    exchange rate:      %s\n", est.ExchangeRate)
	fmt.Printf("    price impact:       %s%%\n", est.AggregatePriceImpact)
	fmt.Printf("    quoteId:            %s\n", resp.QuoteID)

	if !*submit {
		fmt.Printf("\n--submit not set. Quote only, nothing broadcast.\n")
		fmt.Printf("To execute: re-run with --submit (spends %s USDC on %s).\n", whole.String(), sc.name)
		return nil
	}

	// Source-chain RPC required to broadcast.
	rpc := strings.TrimSpace(os.Getenv(sc.rpcEnv))
	if rpc == "" {
		return fmt.Errorf("set %s to a %s RPC URL to broadcast (e.g. your Alchemy endpoint)", sc.rpcEnv, sc.name)
	}

	priv, err := wallet.Export(ctx, db, common.HexToAddress(pdp))
	if err != nil {
		return fmt.Errorf("load pdp key: %w", err)
	}

	fmt.Printf("\nBroadcasting source-chain tx on %s ...\n", sc.name)
	srcHash, err := usdfcacquire.SignAndBroadcast(ctx, usdfcacquire.SourceChain{ChainID: sc.chainID, RPCURL: rpc}, priv, resp.Route.TransactionRequest)
	if err != nil {
		return err
	}
	fmt.Printf("  source tx: %s\n", srcHash)
	fmt.Printf("Tracking cross-chain fill (USDFC lands on Filecoin)...\n")

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	status, werr := client.WaitFilled(waitCtx, srcHash, fmt.Sprintf("%d", sc.chainID), "314", resp.QuoteID, 15*time.Second)
	if werr != nil {
		fmt.Printf("  (still settling: %v) Track later: curio-core doctor, or Squid status with quoteId %s\n", werr, resp.QuoteID)
		return nil
	}
	fmt.Printf("  fill status: %s\n", status)
	fmt.Printf("\nUSDFC should now be in %s. Next: curio-core demo prepare-client-payments --submit\n", pdp)
	return nil
}

func pow10(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// formatToken renders a smallest-unit decimal string with `decs` decimals to
// a 4-place fixed-point string. Best-effort; empty -> "?".
func formatToken(s string, decs int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "?"
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return s
	}
	div := pow10(decs)
	whole := new(big.Int).Quo(v, div)
	frac := new(big.Int).Mod(v, div)
	fracStr := fmt.Sprintf("%0*s", decs, frac.String())
	if len(fracStr) > 4 {
		fracStr = fracStr[:4]
	}
	return whole.String() + "." + fracStr
}
