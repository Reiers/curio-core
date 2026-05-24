// cmd_demo.go - synapse-sdk-shaped end-to-end demo flows.
//
// The "P5" milestone of curio-core#56: drive a real signed-extraData
// createDataSet through the daemon's HTTP API the same way an external
// synapse-sdk client would.
//
// What this does:
//
//  1. Builds the EIP-712 typed-data for FilecoinWarmStorageService's
//     CreateDataSet primary type, matching the message shape in
//     synapse-sdk/packages/synapse-core/src/typed-data/type-definitions.ts
//     and the signing flow in sign-create-dataset.ts.
//  2. Signs the digest with a client wallet (loaded from eth_keys).
//  3. ABI-encodes the extraData per signCreateDataSetAbiParameters:
//     (address payer, uint256 clientDataSetId, string[] keys,
//     string[] values, bytes signature).
//  4. POSTs {recordKeeper: FWSS-proxy, extraData: 0x...} to the daemon's
//     /pdp/data-sets route. The daemon then ABI-encodes a real
//     PDPVerifier.createDataSet(...) call, builds a SenderETH task,
//     and broadcasts on-chain via VMBridge.
//  5. On confirmation, FWSS.dataSetCreated() recovers the signer from
//     the extraData signature and (if payer == recoveredSigner) accepts
//     the dataset, allocating a dataSetId.
//
// Subcommands:
//
//	curio-core demo create-dataset \
//	    [--client-addr <0x...>]          eth_keys signer (default: first 'pdp')
//	    [--record-keeper <0x...>]        defaults to network FWSS proxy
//	    [--metadata key=value ...]       repeatable metadata
//	    [--client-dataset-id <uint>]     EIP-712 nonce (default: random uint256)
//	    [--daemon <url>]                 default http://127.0.0.1:14994
//	    [--network <name>]               default calibration
//	    [--data-dir <path>]              default ~/.curio-core
//	    [--dry-run]                      print payload, do not POST
//
// The same wallet acts as signer/payer (and the SP's registered payee
// is assumed to be that same address for this smoke test, which is
// true on calibration where SP id 25 was registered with this wallet).
// A production client would use a separate signing key.

package main

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	curiocontract "github.com/filecoin-project/curio/pdp/contract"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/wallet"
)

func cmdDemo(args []string) error {
	if len(args) == 0 {
		demoUsage()
		return fmt.Errorf("subcommand required")
	}
	switch args[0] {
	case "create-dataset":
		return cmdDemoCreateDataSet(args[1:])
	case "-h", "--help", "help":
		demoUsage()
		return nil
	default:
		demoUsage()
		return fmt.Errorf("unknown demo subcommand: %s", args[0])
	}
}

func demoUsage() {
	fmt.Fprint(os.Stderr, `curio-core demo

  synapse-sdk-shaped end-to-end demo flows. Produces real EIP-712
  signed extraData payloads and drives them through the daemon's PDP
  HTTP surface the same way an external synapse-sdk client would.

Subcommands:
  create-dataset                      Sign + POST a CreateDataSet payload

Flags (for create-dataset):
  --client-addr <0x...>               signer address (default: first 'pdp' wallet in eth_keys)
  --record-keeper <0x...>             default: network FWSS proxy
  --metadata key=value                 metadata entry (repeatable)
  --client-dataset-id <uint>          EIP-712 nonce (default: random uint256)
  --daemon <url>                      daemon base URL (default: http://127.0.0.1:14994)
  --network <name>                    calibration|mainnet (default: calibration)
  --data-dir <path>                   curio-core data directory (default: ~/.curio-core)
  --dry-run                           print payload, do not POST

`)
}

// metadataFlag accumulates "key=value" arguments into parallel slices.
type metadataFlag struct {
	keys   []string
	values []string
}

func (m *metadataFlag) String() string { return strings.Join(m.keys, ",") }
func (m *metadataFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i < 0 {
		return fmt.Errorf("metadata must be key=value, got %q", v)
	}
	m.keys = append(m.keys, v[:i])
	m.values = append(m.values, v[i+1:])
	return nil
}

// signCreateDataSetAbiParameters mirrors the SDK constant of the same
// name (see synapse-sdk packages/synapse-core/src/typed-data/sign-create-dataset.ts).
var signCreateDataSetAbiParameters = abi.Arguments{
	{Type: mustABIType("address")},
	{Type: mustABIType("uint256")},
	{Type: mustABIType("string[]")},
	{Type: mustABIType("string[]")},
	{Type: mustABIType("bytes")},
}

func mustABIType(t string) abi.Type {
	at, err := abi.NewType(t, "", nil)
	if err != nil {
		panic(fmt.Sprintf("abi.NewType(%q): %v", t, err))
	}
	return at
}

// chainIDFor returns the EIP-155 chain id for the given network. Used
// in the EIP-712 domain.
func chainIDFor(network string) (uint64, error) {
	switch network {
	case "calibration":
		return 314159, nil
	case "mainnet":
		return 314, nil
	default:
		return 0, fmt.Errorf("unknown network %q", network)
	}
}

// fwssAddressFor returns the FilecoinWarmStorageService proxy address
// for the given network from upstream curio's contract package.
func fwssAddressFor(network string) (common.Address, error) {
	var n curiocontract.Network
	switch network {
	case "calibration":
		n = curiocontract.NetworkCalibration
	case "mainnet":
		n = curiocontract.NetworkMainnet
	default:
		return common.Address{}, fmt.Errorf("unknown network %q", network)
	}
	c := curiocontract.ContractAddressesFor(n)
	return c.AllowedPublicRecordKeepers.FWSService, nil
}

func cmdDemoCreateDataSet(args []string) error {
	fs := flag.NewFlagSet("demo create-dataset", flag.ExitOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "curio-core data directory")
	network := fs.String("network", "calibration", "network (calibration|mainnet)")
	daemon := fs.String("daemon", "http://127.0.0.1:14994", "daemon base URL")
	clientAddrStr := fs.String("client-addr", "", "signer address (default: first 'pdp' wallet)")
	recordKeeperStr := fs.String("record-keeper", "", "record-keeper contract (default: network FWSS proxy)")
	clientDataSetIDStr := fs.String("client-dataset-id", "", "EIP-712 nonce (default: random uint256)")
	dryRun := fs.Bool("dry-run", false, "print payload, do not POST")
	var md metadataFlag
	fs.Var(&md, "metadata", "metadata entry key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Resolve the record-keeper (defaults to network FWSS proxy).
	var recordKeeper common.Address
	if *recordKeeperStr != "" {
		recordKeeper = common.HexToAddress(*recordKeeperStr)
	} else {
		fw, err := fwssAddressFor(*network)
		if err != nil {
			return err
		}
		recordKeeper = fw
	}
	chainID, err := chainIDFor(*network)
	if err != nil {
		return err
	}

	// Open eth_keys and load the signing key.
	dbPath := filepath.Join(*dataDir, "state.sqlite")
	db, err := harmonysqlite.Open(harmonysqlite.Config{Path: dbPath})
	if err != nil {
		return fmt.Errorf("opening db at %s: %w", dbPath, err)
	}
	defer db.Close()

	var clientAddr common.Address
	if *clientAddrStr != "" {
		clientAddr = common.HexToAddress(*clientAddrStr)
	} else {
		rows, err := wallet.List(ctx, db)
		if err != nil {
			return fmt.Errorf("listing wallets: %w", err)
		}
		for _, r := range rows {
			if r.Role == "pdp" {
				clientAddr = common.HexToAddress(r.Address)
				break
			}
		}
		if clientAddr == (common.Address{}) {
			return errors.New("no 'pdp' wallet found in eth_keys; use --client-addr or run 'curio-core wallet import'")
		}
	}
	raw, err := wallet.Export(ctx, db, clientAddr)
	if err != nil {
		return fmt.Errorf("exporting key for %s: %w", clientAddr.Hex(), err)
	}
	privKey, err := crypto.ToECDSA(raw)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}
	signerAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	if signerAddr != clientAddr {
		return fmt.Errorf("loaded key for %s does not match db row %s", signerAddr.Hex(), clientAddr.Hex())
	}

	// EIP-712 nonce.
	var clientDataSetID *big.Int
	if *clientDataSetIDStr != "" {
		v, ok := new(big.Int).SetString(*clientDataSetIDStr, 0)
		if !ok {
			return fmt.Errorf("invalid --client-dataset-id %q", *clientDataSetIDStr)
		}
		clientDataSetID = v
	} else {
		clientDataSetID, err = randUint256()
		if err != nil {
			return fmt.Errorf("generating nonce: %w", err)
		}
	}

	// Assumption: payee = signer. True on calibration today (SP id 25
	// was registered via cmd_sp.go using this same wallet as both
	// serviceProvider and payee). For a real client-payer flow we'd
	// query ServiceProviderRegistry.getProviderPayee(providerId).
	payee := signerAddr

	// Build + sign EIP-712.
	td := buildCreateDataSetTypedData(chainID, recordKeeper, clientDataSetID, payee, md.keys, md.values)
	digest, _, err := apitypes.TypedDataAndHash(td)
	if err != nil {
		return fmt.Errorf("hashing typed data: %w", err)
	}
	sig, err := crypto.Sign(digest, privKey)
	if err != nil {
		return fmt.Errorf("signing typed data: %w", err)
	}
	// crypto.Sign emits v in {0,1}; FWSS's recoverSigner accepts both
	// {0,1} and {27,28}, but ethers-shaped sigs use {27,28}. Normalize.
	if sig[64] < 27 {
		sig[64] += 27
	}

	// ABI-encode extraData.
	keys, values := md.keys, md.values
	if keys == nil {
		keys = []string{}
	}
	if values == nil {
		values = []string{}
	}
	extraData, err := signCreateDataSetAbiParameters.Pack(
		signerAddr,      // payer
		clientDataSetID, // uint256
		keys,            // string[]
		values,          // string[]
		sig,             // bytes
	)
	if err != nil {
		return fmt.Errorf("ABI-encoding extraData: %w", err)
	}

	fmt.Println("=== signCreateDataSet ===")
	fmt.Printf("  network          : %s (chainId=%d)\n", *network, chainID)
	fmt.Printf("  daemon           : %s\n", *daemon)
	fmt.Printf("  signer/payer     : %s\n", signerAddr.Hex())
	fmt.Printf("  recordKeeper     : %s (FWSS)\n", recordKeeper.Hex())
	fmt.Printf("  payee (assumed)  : %s\n", payee.Hex())
	fmt.Printf("  clientDataSetId  : 0x%x (%s)\n", clientDataSetID, clientDataSetID)
	fmt.Printf("  metadata entries : %d\n", len(keys))
	for i := range keys {
		fmt.Printf("    [%d] %s=%s\n", i, keys[i], values[i])
	}
	fmt.Printf("  digest           : 0x%s\n", hex.EncodeToString(digest))
	fmt.Printf("  signature        : 0x%s (v=%d)\n", hex.EncodeToString(sig), sig[64])
	fmt.Printf("  extraData        : 0x%s (%d bytes)\n", hex.EncodeToString(extraData), len(extraData))

	reqBody := map[string]string{
		"recordKeeper": recordKeeper.Hex(),
		"extraData":    "0x" + hex.EncodeToString(extraData),
	}

	if *dryRun {
		bj, _ := json.MarshalIndent(reqBody, "  ", "  ")
		fmt.Println("\n--dry-run: not POSTing. Request body would be:")
		fmt.Printf("  %s\n", string(bj))
		return nil
	}

	// POST to /pdp/data-sets.
	bj, _ := json.Marshal(reqBody)
	postURL := strings.TrimRight(*daemon, "/") + "/pdp/data-sets"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(bj))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	fmt.Printf("\nPOST %s ...\n", postURL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("  HTTP %d\n", resp.StatusCode)
	if loc := resp.Header.Get("Location"); loc != "" {
		fmt.Printf("  Location: %s\n", loc)
		parts := strings.Split(loc, "/")
		if len(parts) > 0 {
			txHash := parts[len(parts)-1]
			fmt.Printf("  Watch on calibration:  https://calibration.filfox.info/en/message/%s\n", txHash)
			fmt.Printf("  Poll dataset status:   curl %s%s\n", strings.TrimRight(*daemon, "/"), loc)
		}
	}
	if len(respBody) > 0 {
		fmt.Printf("  Body: %s\n", string(respBody))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("daemon rejected request (status %d)", resp.StatusCode)
	}
	return nil
}

// buildCreateDataSetTypedData mirrors the SDK's EIP712Types + CreateDataSet
// primary type from synapse-sdk packages/synapse-core/src/typed-data/type-definitions.ts.
func buildCreateDataSetTypedData(chainID uint64, verifyingContract common.Address, clientDataSetID *big.Int, payee common.Address, keys, values []string) apitypes.TypedData {
	mdEntries := make([]apitypes.TypedDataMessage, len(keys))
	for i := range keys {
		mdEntries[i] = apitypes.TypedDataMessage{
			"key":   keys[i],
			"value": values[i],
		}
	}
	mdAny := make([]any, len(mdEntries))
	for i := range mdEntries {
		mdAny[i] = mdEntries[i]
	}
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"MetadataEntry": []apitypes.Type{
				{Name: "key", Type: "string"},
				{Name: "value", Type: "string"},
			},
			"CreateDataSet": []apitypes.Type{
				{Name: "clientDataSetId", Type: "uint256"},
				{Name: "payee", Type: "address"},
				{Name: "metadata", Type: "MetadataEntry[]"},
			},
		},
		PrimaryType: "CreateDataSet",
		Domain: apitypes.TypedDataDomain{
			Name:              "FilecoinWarmStorageService",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(int64(chainID)),
			VerifyingContract: verifyingContract.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			// apitypes wants uint256 as a string (decimal or hex).
			"clientDataSetId": clientDataSetID.String(),
			"payee":           payee.Hex(),
			"metadata":        mdAny,
		},
	}
}

// randUint256 returns a uniformly random 256-bit value, matching the
// SDK's randU256() default for clientDataSetId.
func randUint256() (*big.Int, error) {
	var b [32]byte
	if _, err := io.ReadFull(cryptorand.Reader, b[:]); err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(b[:]), nil
}
