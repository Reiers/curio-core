// cmd_sp.go — SP Registry operations.
//
//	curio-core sp info  [--address <0xhex>]    look up SP registration
//	                                            (default: the pdp wallet)
//	curio-core sp register --name <s> --description <s>
//	                       --service-url <url>
//	                       [--location <s>] [--min-piece-size <n>]
//	                       [--max-piece-size <n>] [--storage-price-per-tib-per-day <n>]
//	                       [--min-proving-period-epochs <n>] [--payment-token <addr>]
//	                       [--payee <0xhex>]
//	                       (--dry-run | --submit)
//
// Day 8 P2 milestone: --submit posts to the running daemon's
// /admin/test-tx endpoint, which builds a SenderETH harmonytask. The
// scheduler then broadcasts via embedded Lantern's VMBridge.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
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
  register --name <s> --description <s> --service-url <url>
           [--location <s>] [--min-piece-size <n>] [--max-piece-size <n>]
           [--storage-price-per-tib-per-day <n>] [--min-proving-period-epochs <n>]
           [--payment-token <addr>] [--payee <0xhex>]
           (--dry-run | --submit) [--admin-endpoint <url>]
                                       Register this SP. --dry-run prints calldata
                                       for manual submission. --submit POSTs to
                                       the running daemon's /admin/test-tx, which
                                       builds a SenderETH harmonytask (~5 FIL fee).

Common flags:
  --data-dir <path>                    curio-core data directory
  --network <n>                        mainnet | calibration (default: calibration)
  --gateway <url>                      Lantern gateway URL
  --vm-bridge-rpc <url>                VM bridge upstream

Contracts (FilOzone ServiceProviderRegistry):
  calibration: 0x839e5c9988e4e9977d40708d0094103c0839Ac9D
  mainnet:     0xf55dDbf63F1b55c3F1D4FA7e339a68AB7b64A5eB
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

// SP registry REGISTRATION_FEE on calibration / mainnet: 5 FIL. Verified
// via eth_call REGISTRATION_FEE() against the deployed contract; tracked
// at the call site so any future contract upgrade is easy to spot.
const spRegistrationFeeWeiHex = "0x4563918244f40000" // 5 * 10^18

// Required PDPv0 capability keys per the deployed ServiceProviderRegistry
// contract (see FilOzone/filecoin-services/service_contracts/src/
// ServiceProviderRegistry.sol REQUIRED_PDP_KEYS bloom filter).
//
// All 7 must be present in capabilityKeys[] or registerProvider reverts
// with InsufficientCapabilitiesForProduct(0).
var pdpv0RequiredCapKeys = []string{
	"serviceURL",
	"minPieceSizeInBytes",
	"maxPieceSizeInBytes",
	"storagePricePerTibPerDay",
	"minProvingPeriodInEpochs",
	"location",
	"paymentTokenAddress",
}

func cmdSPRegister(args []string) error {
	fs := flag.NewFlagSet("sp register", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "Data directory")
	network := fs.String("network", string(lanternbuild.DefaultNetwork), "Network")
	name := fs.String("name", "", "Public-facing SP name (required)")
	description := fs.String("description", "", "Public-facing SP description (required)")
	payee := fs.String("payee", "", "Payee address (default: the pdp wallet)")
	serviceURL := fs.String("service-url", "", "PDP service URL (required; e.g. https://pdp-test.reiers.io)")
	location := fs.String("location", "", "Geographic location (X.509 DN-ish, e.g. C=FI;ST=Uusimaa;L=Helsinki)")
	minPieceSize := fs.Int64("min-piece-size", 256, "Minimum piece size in bytes")
	maxPieceSize := fs.Int64("max-piece-size", 34359738368, "Maximum piece size in bytes (default 32 GiB)")
	storagePrice := fs.Int64("storage-price-per-tib-per-day", 0, "Storage price per TiB per day in token smallest unit (0 = delegate to CIDgravity/FoC)")
	provingPeriod := fs.Int64("min-proving-period-epochs", 1440, "Minimum proving period in epochs (default 1440 = ~12h)")
	paymentToken := fs.String("payment-token", "0x0000000000000000000000000000000000000000", "Payment token contract address (0x0 = native FIL)")
	dryRun := fs.Bool("dry-run", false, "Print calldata + do NOT broadcast")
	submit := fs.Bool("submit", false, "Broadcast via the running curio-core daemon's /admin/test-tx (requires daemon running)")
	adminEndpoint := fs.String("admin-endpoint", "http://127.0.0.1:14994", "curio-core daemon admin endpoint (loopback)")
	fs.Parse(args)

	if *name == "" || *description == "" {
		return fmt.Errorf("--name and --description are required")
	}
	if !*dryRun && !*submit {
		return fmt.Errorf("choose one of --dry-run (print calldata) or --submit (broadcast via running daemon)")
	}
	if *dryRun && *submit {
		return fmt.Errorf("--dry-run and --submit are mutually exclusive")
	}
	if *submit && *serviceURL == "" {
		return fmt.Errorf("--service-url is required to submit (the registry rejects empty serviceURL)")
	}
	if *paymentToken != "" && !common.IsHexAddress(*paymentToken) {
		return fmt.Errorf("not a valid --payment-token address: %q", *paymentToken)
	}
	paymentTokenAddr := common.HexToAddress(*paymentToken)

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

	productType := uint8(0) // PDPv0

	// Build the 7 required capability key/value pairs. Values are ABI-
	// encoded as raw bytes per the contract's bytes[] type: addresses are
	// 20-byte big-endian, integers are big-endian with minimal width
	// (matches kubuxu's calibration registration, the closest production
	// reference).
	capKeys := append([]string(nil), pdpv0RequiredCapKeys...)
	capValues := [][]byte{
		[]byte(*serviceURL),
		bigEndianUint(uint64(*minPieceSize)),
		bigEndianUint(uint64(*maxPieceSize)),
		bigEndianUint(uint64(*storagePrice)),
		bigEndianUint(uint64(*provingPeriod)),
		[]byte(*location),
		paymentTokenAddr.Bytes(),
	}

	abiData, err := contract.ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("load registry ABI: %w", err)
	}
	calldata, err := abiData.Pack("registerProvider", payeeAddr, *name, *description, productType, capKeys, capValues)
	if err != nil {
		return fmt.Errorf("pack calldata: %w", err)
	}

	fmt.Printf("SP Registration\n")
	fmt.Printf("  network:                       %s\n", *network)
	fmt.Printf("  registry:                      %s\n", registryAddr.Hex())
	fmt.Printf("  from (pdp):                    %s\n", from.Hex())
	fmt.Printf("  payee:                         %s\n", payeeAddr.Hex())
	fmt.Printf("  name:                          %s\n", *name)
	fmt.Printf("  description:                   %s\n", *description)
	fmt.Printf("  productType:                   %d (PDPv0)\n", productType)
	fmt.Printf("  serviceURL:                    %s\n", *serviceURL)
	fmt.Printf("  location:                      %s\n", *location)
	fmt.Printf("  minPieceSizeInBytes:           %d\n", *minPieceSize)
	fmt.Printf("  maxPieceSizeInBytes:           %d\n", *maxPieceSize)
	fmt.Printf("  storagePricePerTibPerDay:      %d (0 = delegated to CIDgravity)\n", *storagePrice)
	fmt.Printf("  minProvingPeriodInEpochs:      %d\n", *provingPeriod)
	fmt.Printf("  paymentTokenAddress:           %s (0x0=FIL)\n", paymentTokenAddr.Hex())
	fmt.Printf("  value:                         5 FIL (REGISTRATION_FEE)\n")
	fmt.Printf("  calldata (hex, %d bytes):\n", len(calldata))
	fmt.Printf("  0x%x\n\n", calldata)

	if *dryRun {
		fmt.Println("--dry-run set. No tx broadcast.")
		fmt.Println()
		fmt.Println("To submit via the running daemon:")
		fmt.Println("  curio-core sp register --submit  (with the same args minus --dry-run)")
		fmt.Println()
		fmt.Println("To submit manually with cast (foundry):")
		fmt.Printf("  cast send %s 0x%x \\\n    --value 5ether \\\n    --rpc-url <calibration-rpc> --private-key <pdp-key>\n",
			registryAddr.Hex(), calldata)
		return nil
	}

	// --submit: POST to the running daemon's /admin/test-tx. That handler
	// builds a SenderETH harmonytask with the given to/value/data; the
	// scheduler claims it and broadcasts via embedded Lantern's
	// eth_sendRawTransaction.
	body := map[string]string{
		"to":    registryAddr.Hex(),
		"value": spRegistrationFeeWeiHex,
		"data":  fmt.Sprintf("0x%x", calldata),
	}
	bodyJSON, _ := json.Marshal(body)
	url := *adminEndpoint + "/admin/test-tx"
	fmt.Printf("Submitting via %s ...\n", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin endpoint returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var adminResp struct {
		From   string `json:"from"`
		TxHash string `json:"txHash"`
	}
	if err := json.Unmarshal(respBody, &adminResp); err != nil {
		return fmt.Errorf("decode admin response: %w (body: %s)", err, string(respBody))
	}
	fmt.Printf("Submitted.\n")
	fmt.Printf("  txHash:                        %s\n", adminResp.TxHash)
	fmt.Printf("  from:                          %s\n", adminResp.From)
	fmt.Println()
	fmt.Println("Watch the tx on calibration block explorer:")
	fmt.Printf("  https://calibration.filfox.info/en/message/%s\n", adminResp.TxHash)
	fmt.Println()
	fmt.Println("Or query: curio-core sp info  (once the tx confirms, ~1-2 minutes)")
	return nil
}

// bigEndianUint returns the minimal big-endian representation of v (no
// leading zero bytes). Matches how the deployed ServiceProviderRegistry
// stores integer capability values (see kubuxu's calibration
// registration for reference shape).
func bigEndianUint(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 8)
	for v > 0 {
		out = append([]byte{byte(v & 0xff)}, out...)
		v >>= 8
	}
	return out
}

// --- helpers ---------------------------------------------------------

// spRegistryAddressFor returns the ServiceProviderRegistry proxy address
// for the given network. Source: FilOzone/filecoin-services
// service_contracts/deployments.json (commit ed85348e, chain IDs 314 +
// 314159).
func spRegistryAddressFor(network string) (common.Address, bool) {
	switch network {
	case "calibration":
		return common.HexToAddress("0x839e5c9988e4e9977d40708d0094103c0839Ac9D"), true
	case "mainnet":
		return common.HexToAddress("0xf55dDbf63F1b55c3F1D4FA7e339a68AB7b64A5eB"), true
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
