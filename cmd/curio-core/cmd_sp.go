// cmd_sp.go — SP Registry operations.
//
//	curio-core sp info  [--address <0xhex>]    look up SP registration
//	                                            (default: the pdp wallet)
//	curio-core sp register --name <s> --description <s>
//	                       [--payee <0xhex>]
//	                       [--dry-run]          (default; mutation is gated)
//
// v0.1: info is fully wired; register is dry-run-only until SenderETH
// harmonytask processing lands (#17 unblocks). The dry-run mode prints
// the exact calldata that would be broadcast, so an operator can
// submit it manually with their own tooling in the meantime.

package main

import (
	"context"
	"flag"
	"fmt"
	"math/big"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	lanternbuild "github.com/Reiers/lantern/build"
	lantern "github.com/Reiers/lantern/pkg/daemon"
	lanternwallet "github.com/Reiers/lantern/wallet"

	"github.com/filecoin-project/curio/pdp/contract"

	cethclient "github.com/Reiers/curio-core/internal/ethclient"
	"github.com/Reiers/curio-core/internal/ethkeys"
)

func cmdSP(args []string) error {
	if len(args) == 0 {
		spUsage()
		return fmt.Errorf("subcommand required")
	}
	switch args[0] {
	case "info":
		return cmdSPInfo(args[1:])
	case "register":
		return cmdSPRegister(args[1:])
	case "-h", "--help", "help":
		spUsage()
		return nil
	default:
		spUsage()
		return fmt.Errorf("unknown sp subcommand: %s", args[0])
	}
}

func spUsage() {
	fmt.Print(`curio-core sp

  Service Provider Registry operations (FilOzone ServiceProviderRegistry contract).

Subcommands:
  info [--address <0xhex>]            Look up SP registration on chain.
                                       Default address: the pdp wallet from eth_keys.
  register --name <s> --description <s> [--payee <0xhex>] [--dry-run]
                                       Register this SP. v0.1 is --dry-run-only
                                       (prints calldata for manual submission;
                                       on-chain submit lands after #17).

Common flags:
  --data-dir <path>                    curio-core data directory
  --network <n>                        mainnet | calibration (default: calibration)
  --gateway <url>                      Lantern gateway URL
  --vm-bridge-rpc <url>                VM bridge upstream

Contracts (FilOzone ServiceProviderRegistry):
  calibration: 0x839e5c9988e4e9977d40708d0094103c0839Ac9D
  mainnet:     (not yet deployed)
`)
}

// --- info ------------------------------------------------------------

func cmdSPInfo(args []string) error {
	fs := flag.NewFlagSet("sp info", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Network")
	gateway := fs.String("gateway", "", "Gateway URL")
	vmBridgeRPC := fs.String("vm-bridge-rpc", "", "VM bridge URL")
	addressFlag := fs.String("address", "", "Address to query (default: pdp wallet)")
	timeout := fs.Duration("timeout", 45*time.Second, "Total timeout")
	fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	queryAddr := *addressFlag
	if queryAddr == "" {
		pdpAddr, err := ethkeys.LookupPDP(ctx, db)
		if err != nil {
			return fmt.Errorf("lookup pdp wallet: %w", err)
		}
		if pdpAddr == "" {
			return fmt.Errorf("no --address provided and no pdp wallet in eth_keys (run 'curio-core wallet new --role pdp' first)")
		}
		queryAddr = pdpAddr
	}
	if !common.IsHexAddress(queryAddr) {
		return fmt.Errorf("not a valid address: %q", queryAddr)
	}
	addr := common.HexToAddress(queryAddr)

	eth, stop, err := bootLanternForRead(ctx, *dataDir, *network, *gateway, *vmBridgeRPC)
	if err != nil {
		return err
	}
	defer stop()

	registryAddr, ok := spRegistryAddressFor(*network)
	if !ok {
		return fmt.Errorf("no SP Registry deployed on %s yet", *network)
	}
	fmt.Printf("Querying SP Registry %s on %s for %s\n\n", registryAddr.Hex(), *network, addr.Hex())

	caller, err := contract.NewServiceProviderRegistryCaller(registryAddr, eth)
	if err != nil {
		return fmt.Errorf("build registry caller: %w", err)
	}

	providerID, err := caller.GetProviderIdByAddress(nil, addr)
	if err != nil {
		return fmt.Errorf("GetProviderIdByAddress: %w", err)
	}
	if providerID == nil || providerID.Sign() == 0 {
		fmt.Println("  ⓘ this address is NOT registered as an SP yet.")
		fmt.Println("  use 'curio-core sp register --name ... --description ... --dry-run' to draft calldata.")
		return nil
	}
	fmt.Printf("  provider id:  %s\n", providerID.String())

	info, err := caller.GetProviderByAddress(nil, addr)
	if err != nil {
		fmt.Printf("  WARN: GetProviderByAddress failed: %v\n", err)
		return nil
	}
	fmt.Printf("  name:         %s\n", info.Info.Name)
	fmt.Printf("  description:  %s\n", info.Info.Description)
	fmt.Printf("  payee:        %s\n", info.Info.Payee.Hex())
	fmt.Printf("  active:       %v\n", info.Info.IsActive)
	return nil
}

// --- register (dry-run only for v0.1) -------------------------------

func cmdSPRegister(args []string) error {
	fs := flag.NewFlagSet("sp register", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Network")
	name := fs.String("name", "", "Public-facing SP name (required)")
	description := fs.String("description", "", "Public-facing SP description (required)")
	payee := fs.String("payee", "", "Payee address (default: the pdp wallet)")
	dryRun := fs.Bool("dry-run", true, "Print calldata instead of broadcasting. v0.1 always dry-runs.")
	fs.Parse(args)

	if *name == "" || *description == "" {
		return fmt.Errorf("--name and --description are required")
	}

	ctx := context.Background()
	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer closeDB()

	// Resolve from + payee.
	pdpAddr, err := ethkeys.LookupPDP(ctx, db)
	if err != nil {
		return err
	}
	if pdpAddr == "" {
		return fmt.Errorf("no pdp wallet in eth_keys")
	}
	from := common.HexToAddress(pdpAddr)
	payeeAddr := from
	if *payee != "" {
		if !common.IsHexAddress(*payee) {
			return fmt.Errorf("not a valid --payee address: %q", *payee)
		}
		payeeAddr = common.HexToAddress(*payee)
	}

	registryAddr, ok := spRegistryAddressFor(*network)
	if !ok {
		return fmt.Errorf("no SP Registry deployed on %s yet", *network)
	}

	// productType: 0 = PDPv0 (per FilOzone enum at time of writing).
	productType := uint8(0)
	capKeys := []string{}
	capValues := [][]byte{}

	abiData, err := contract.ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("load registry ABI: %w", err)
	}
	calldata, err := abiData.Pack("registerProvider", payeeAddr, *name, *description, productType, capKeys, capValues)
	if err != nil {
		return fmt.Errorf("pack calldata: %w", err)
	}

	fmt.Printf("SP Registration draft\n")
	fmt.Printf("  network:      %s\n", *network)
	fmt.Printf("  registry:     %s\n", registryAddr.Hex())
	fmt.Printf("  from (pdp):   %s\n", from.Hex())
	fmt.Printf("  payee:        %s\n", payeeAddr.Hex())
	fmt.Printf("  name:         %s\n", *name)
	fmt.Printf("  description:  %s\n", *description)
	fmt.Printf("  productType:  %d (PDPv0)\n", productType)
	fmt.Printf("  capabilities: (none)\n\n")
	fmt.Printf("  calldata (hex, %d bytes):\n", len(calldata))
	fmt.Printf("  0x%x\n\n", calldata)

	if *dryRun {
		fmt.Println("--dry-run set (default). No tx broadcast.")
		fmt.Println()
		fmt.Println("To submit manually with cast (foundry):")
		fmt.Printf("  cast send %s 0x%x --rpc-url <calibration-rpc> --private-key <pdp-key>\n",
			registryAddr.Hex(), calldata)
		fmt.Println()
		fmt.Println("Once #17 (harmonysqlite time-column scan) lands, this command will")
		fmt.Println("submit via the embedded SenderETH harmonytask path automatically.")
		return nil
	}

	return fmt.Errorf("non-dry-run mode requires #17 (SenderETH harmonytask). use --dry-run for v0.1")
}

// --- helpers ---------------------------------------------------------

func spRegistryAddressFor(network string) (common.Address, bool) {
	switch network {
	case "calibration":
		return common.HexToAddress("0x839e5c9988e4e9977d40708d0094103c0839Ac9D"), true
	}
	return common.Address{}, false
}

// bootLanternForRead spins up a temporary embedded Lantern daemon for
// read-only chain access. Returns the ethclient + a stop func.
//
// Shared between sp + doctor; pulled into a single helper here so both
// command paths boot the daemon identically.
func bootLanternForRead(ctx context.Context, dataDir, network, gateway, vmBridgeRPC string) (*cethclient.Client, func(), error) {
	bridgeURL := vmBridgeRPC
	if bridgeURL == "" {
		switch network {
		case "calibration":
			bridgeURL = "https://api.calibration.node.glif.io/rpc/v1"
		case "mainnet":
			bridgeURL = "https://api.node.glif.io/rpc/v1"
		}
	}

	w, err := lanternwallet.New(ctx, filepath.Join(dataDir, "keystore"), "")
	if err != nil {
		return nil, nil, fmt.Errorf("open keystore: %w", err)
	}

	d, err := lantern.New(lantern.Config{
		DataDir:      dataDir,
		Wallet:       w,
		Gateway:      gateway,
		Network:      network,
		RPCListen:    "127.0.0.1:0",
		NoLibp2p:     true,
		EmbeddedMode: true,
		VMBridgeRPC:  bridgeURL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build lantern: %w", err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if d.Started() {
			break
		}
		select {
		case e := <-errCh:
			return nil, nil, fmt.Errorf("lantern exited before Started: %w", e)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !d.Started() {
		return nil, nil, fmt.Errorf("lantern did not reach Started in 20s")
	}

	eth, err := cethclient.New(ctx, d)
	if err != nil {
		_ = d.Stop(context.Background())
		return nil, nil, fmt.Errorf("dial embedded lantern eth: %w", err)
	}
	stop := func() {
		eth.Close()
		_ = d.Stop(context.Background())
	}
	return eth, stop, nil
}

// Silence import-not-used in case crypto is removed later.
var _ = crypto.Keccak256
var _ = big.NewInt
