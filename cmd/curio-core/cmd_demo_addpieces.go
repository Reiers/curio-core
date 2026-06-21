// cmd_demo_addpieces.go — synapse-sdk-shaped addPieces demo flow.
//
//	curio-core demo add-pieces \
//	    --data-set-id <uint>            on-chain dataset id (from create-dataset)
//	    --piece-cid <baga6ea4...>       repeatable, one per piece to add
//	    [--metadata <key=val>]          repeatable, applied to ALL pieces uniformly
//	    [--data-dir <path>]
//	    [--daemon <url>]
//	    [--network calibration|mainnet]
//	    [--dry-run]
//
// Builds the EIP-712 typed-data for FilecoinWarmStorageService's AddPieces
// primary type, signs with the pdp wallet, ABI-encodes the extraData per
// synapse-sdk's signAddPiecesAbiParameters shape, and POSTs the request
// to /pdp/data-sets/{set-id}/pieces.
//
// The daemon then:
//   1. ABI-encodes a PDPVerifier.addPieces(setId, pieceData[], extraData) call
//   2. Builds a SenderETH harmonytask to broadcast on-chain
//   3. Returns Location: /pdp/data-sets/{set-id}/pieces/added/{txHash}
//   4. On confirmation, FWSS.dataSetAddPieces() recovers the payer from
//      the extraData signature, validates it matches the dataset's payer,
//      and inserts the pieces into the on-chain set.
//   5. The dataset_watch / addPieces watcher inserts into pdp_data_set_pieces.
//   6. SaveCache fires for each piece (needs_save_cache=TRUE), builds the
//      Merkle layer cache in pdp_cache_layer.

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
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/ipfs/go-cid"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/pdp/contract"
)

// looksLikePieceCidV1 distinguishes v1 (`baga6ea4se...`, multicodec
// 0xf101 fil-commitment-unsealed) from v2 (multicodec 0x1011 raw-piece-
// commitment with the FRC-0066 size+height-padded form). v1 CIDs are
// what live in parked_pieces today; v2 is what FWSS expects.
func looksLikePieceCidV1(c cid.Cid) bool {
	// v1 piece commitments use codec 0xf101 (fil-commitment-unsealed).
	return c.Type() == 0xf101
}

// signAddPiecesAbiParameters mirrors the SDK shape:
//
//	(uint256 nonce, string[][] keys, string[][] values, bytes signature)
//
// (synapse-sdk packages/synapse-core/src/typed-data/sign-add-pieces.ts)
var signAddPiecesAbiParameters = abi.Arguments{
	{Type: mustABIType("uint256")},
	{Type: mustABIType("string[][]")},
	{Type: mustABIType("string[][]")},
	{Type: mustABIType("bytes")},
}

// multiStringSliceFlag accumulates --piece-cid foo --piece-cid bar...
type multiStringSliceFlag []string

func (m *multiStringSliceFlag) String() string { return strings.Join(*m, ",") }
func (m *multiStringSliceFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func cmdDemoAddPieces(args []string) error {
	fs := flag.NewFlagSet("demo add-pieces", flag.ContinueOnError)
	dataSetID := fs.Uint64("data-set-id", 0, "on-chain dataset id (from create-dataset)")
	var pieceCIDs multiStringSliceFlag
	fs.Var(&pieceCIDs, "piece-cid", "piece CID to add (repeatable)")
	var metaFlags multiStringSliceFlag
	fs.Var(&metaFlags, "metadata", "metadata entry key=value (repeatable, applied to all pieces uniformly)")
	dataDir := fs.String("data-dir", defaultDataDir(), "curio-core data directory")
	network := fs.String("network", "calibration", "network (calibration|mainnet)")
	daemon := fs.String("daemon", "http://127.0.0.1:4711", "daemon base URL (PDP /pdp endpoints)")
	rpc := fs.String("rpc", "", "node JSON-RPC base URL for on-chain reads (default: --daemon; use the RPC port in split-port mode)")
	dryRun := fs.Bool("dry-run", false, "print payload without POSTing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dataSetID == 0 {
		return errors.New("--data-set-id is required (from a successful create-dataset run)")
	}
	if len(pieceCIDs) == 0 {
		return errors.New("at least one --piece-cid is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Resolve client/payer wallet.
	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return fmt.Errorf("open wallet DB: %w", err)
	}
	var privBytes []byte
	if err := db.QueryRowI(ctx, `SELECT private_key FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&privBytes); err != nil {
		closeDB()
		return fmt.Errorf("read pdp wallet: %w", err)
	}
	closeDB()
	privKey, err := crypto.ToECDSA(privBytes)
	if err != nil {
		return fmt.Errorf("decode pdp wallet: %w", err)
	}

	// Look up the dataset's clientDataSetId. The PG/SQLite schema stores
	// the dataset row keyed by the on-chain id but the EIP-712 sig uses
	// the client-chosen clientDataSetId (a uint256 nonce per dataset).
	// For our demo, the dataset was created with a random uint256 in the
	// previous step; we re-read it from pdp_data_set_creates if we have
	// it, otherwise the caller must pass it as a flag.
	//
	// In the current schema we don't store clientDataSetId per dataset
	// in curio-core. The simplest path: regenerate it deterministically
	// from the create_message_hash if needed, OR have the caller pass
	// it. For the smoke run we'll pass it via flag.
	//
	// Actually the dataset's clientDataSetId is on-chain in FWSS at:
	//   dataSetInfo[dataSetId].clientDataSetId
	// We could eth_call it, but the daemon does NOT need it — only the
	// CLIENT does, for the EIP-712 signature. Since we're the same
	// wallet, we can just generate a fresh nonce for this addPieces
	// signature (the contract just checks nonce hasn't been used
	// before by this clientDataSetId).
	//
	// Hmm, but the EIP-712 message needs clientDataSetId too. Easiest:
	// expose --client-dataset-id flag and require it from the CLI; the
	// caller (this script) knows what they used at create-dataset time.
	//
	// For now: query FWSS on-chain to get it.
	pdpContracts := contract.ContractAddressesFor(contract.Network(*network))
	fwss := pdpContracts.AllowedPublicRecordKeepers.FWSService
	chainIDU64, err := chainIDFor(*network)
	if err != nil {
		return fmt.Errorf("chainIDFor(%q): %w", *network, err)
	}
	chainID := int(chainIDU64)

	rpcBase := *rpc
	if rpcBase == "" {
		rpcBase = *daemon
	}
	clientDataSetID, err := lookupClientDataSetID(ctx, rpcBase, *network, *dataSetID)
	if err != nil {
		return fmt.Errorf("look up clientDataSetId for dataset %d: %w", *dataSetID, err)
	}

	// Use a random uint256 nonce for the addPieces signature. The FWSS
	// contract tracks nonces per (payer, clientDataSetId) pair.
	nonce, err := randUint256()
	if err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	// Parse pieces. Each piece CID -> raw bytes for the EIP-712 message.
	// We accept piece CID v1 (`baga6ea4se...`) on input and upgrade to v2
	// using the parked_pieces.piece_raw_size we stored locally. The chain
	// expects the v2 CID (per FRC-0066) but most operator-facing tooling
	// still emits v1; the conversion is deterministic given rawSize.
	pieceCIDsParsed := make([]cid.Cid, len(pieceCIDs))
	db2, closeDB2, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return fmt.Errorf("reopen wallet DB for piece-size lookup: %w", err)
	}
	defer closeDB2()
	for i, pc := range pieceCIDs {
		c, err := cid.Decode(pc)
		if err != nil {
			return fmt.Errorf("decode --piece-cid[%d] %q: %w", i, pc, err)
		}
		if looksLikePieceCidV1(c) {
			// Look up rawSize from parked_pieces, convert to v2.
			var rawSize uint64
			if err := db2.QueryRowI(ctx, `SELECT piece_raw_size FROM parked_pieces WHERE piece_cid = $1 LIMIT 1`, pc).Scan(&rawSize); err != nil {
				return fmt.Errorf("piece %s: cannot resolve raw size from parked_pieces (need a v1->v2 upgrade): %w", pc, err)
			}
			v2, err := commcid.PieceCidV2FromV1(c, rawSize)
			if err != nil {
				return fmt.Errorf("piece %s: v1->v2 upgrade failed: %w", pc, err)
			}
			fmt.Printf("  upgraded v1 -> v2: %s -> %s (raw size %d)\n", pc, v2.String(), rawSize)
			c = v2
			pieceCIDs[i] = v2.String() // for the POST body
		}
		pieceCIDsParsed[i] = c
	}

	// Parse uniform metadata.
	metaKeys, metaValues, err := parseMetadataPairs(metaFlags)
	if err != nil {
		return fmt.Errorf("parse --metadata: %w", err)
	}

	// Build the EIP-712 typed-data.
	typedData := buildAddPiecesTypedData(chainID, fwss, clientDataSetID, nonce, pieceCIDsParsed, metaKeys, metaValues)

	// Sign.
	digest, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return fmt.Errorf("EIP-712 hash: %w", err)
	}
	sig, err := crypto.Sign(digest, privKey)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	if len(sig) != 65 {
		return fmt.Errorf("unexpected sig length %d", len(sig))
	}
	sig[64] += 27

	// ABI-encode extraData = (nonce, string[][] keys, string[][] values, signature)
	// Each piece gets its own keys/values inner slice. With uniform
	// metadata across all pieces we replicate the keys/values N times.
	keys := make([][]string, len(pieceCIDs))
	values := make([][]string, len(pieceCIDs))
	for i := range pieceCIDs {
		keys[i] = metaKeys
		values[i] = metaValues
	}
	extraData, err := signAddPiecesAbiParameters.Pack(nonce, keys, values, sig)
	if err != nil {
		return fmt.Errorf("ABI-encode extraData: %w", err)
	}

	clientAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	fmt.Println("=== signAddPieces ===")
	fmt.Printf("  network          : %s (chainId=%d)\n", *network, chainID)
	fmt.Printf("  daemon           : %s\n", *daemon)
	fmt.Printf("  signer/payer     : %s\n", clientAddr.Hex())
	fmt.Printf("  dataSetId        : %d (on-chain)\n", *dataSetID)
	fmt.Printf("  clientDataSetId  : 0x%x  (%s)\n", clientDataSetID, clientDataSetID.String())
	fmt.Printf("  nonce            : 0x%x\n", nonce)
	fmt.Printf("  pieces (%d):\n", len(pieceCIDs))
	for i, pc := range pieceCIDs {
		fmt.Printf("    [%d] %s\n", i, pc)
	}
	fmt.Printf("  metadata entries : %d (applied uniformly to all pieces)\n", len(metaKeys))
	fmt.Printf("  digest           : 0x%x\n", digest)
	fmt.Printf("  signature        : 0x%x (v=%d)\n", sig, sig[64])
	fmt.Printf("  extraData        : 0x%x (%d bytes)\n", extraData, len(extraData))
	fmt.Println()

	// Build POST body: {pieces: [{pieceCid, subPieces[]}], extraData}.
	// Upstream's handler requires at least one subPiece per piece. For
	// the simplest single-piece-as-its-own-aggregate case, the subPiece
	// IS the piece itself (the piece is a 1-leaf Merkle commitment).
	type SubPieceEntry struct {
		SubPieceCID string `json:"subPieceCid"`
	}
	type AddPieceRequest struct {
		PieceCID  string          `json:"pieceCid"`
		SubPieces []SubPieceEntry `json:"subPieces"`
	}
	type AddPiecesPayload struct {
		Pieces    []AddPieceRequest `json:"pieces"`
		ExtraData string            `json:"extraData"`
	}
	body := AddPiecesPayload{
		ExtraData: "0x" + hex.EncodeToString(extraData),
	}
	for _, pc := range pieceCIDs {
		body.Pieces = append(body.Pieces, AddPieceRequest{
			PieceCID:  pc,
			SubPieces: []SubPieceEntry{{SubPieceCID: pc}}, // single-leaf: piece is its own subPiece
		})
	}

	if *dryRun {
		fmt.Println("--dry-run: not submitting. POST this payload to /pdp/data-sets/{id}/pieces:")
		bj, _ := json.MarshalIndent(body, "", "  ")
		fmt.Println(string(bj))
		return nil
	}

	url := fmt.Sprintf("%s/pdp/data-sets/%d/pieces", strings.TrimRight(*daemon, "/"), *dataSetID)
	bj, _ := json.Marshal(body)
	fmt.Printf("POST %s ...\n", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bj))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("  HTTP %d\n", resp.StatusCode)
	if len(respBody) > 0 {
		fmt.Printf("  Body: %s\n", string(respBody))
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		fmt.Printf("  Location: %s\n", loc)
		parts := strings.Split(loc, "/")
		if len(parts) > 0 {
			txHash := parts[len(parts)-1]
			fmt.Printf("  Watch:                 %s\n", explorerMessageURL(*network, txHash))
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("daemon rejected request (status %d)", resp.StatusCode)
	}
	return nil
}

// parseMetadataPairs converts []"k=v","k2=v2" -> (keys, values).
func parseMetadataPairs(pairs []string) (keys, values []string, err error) {
	keys = []string{}
	values = []string{}
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq <= 0 {
			return nil, nil, fmt.Errorf("metadata entry %q is not key=value", p)
		}
		keys = append(keys, strings.TrimSpace(p[:eq]))
		values = append(values, strings.TrimSpace(p[eq+1:]))
	}
	return keys, values, nil
}

// buildAddPiecesTypedData constructs the EIP-712 typed-data for
// FilecoinWarmStorageService.AddPieces. Mirrors the SDK shape in
// synapse-sdk/packages/synapse-core/src/typed-data/type-definitions.ts.
func buildAddPiecesTypedData(
	chainID int,
	fwss common.Address,
	clientDataSetID *big.Int,
	nonce *big.Int,
	pieceCIDs []cid.Cid,
	metaKeys, metaValues []string,
) apitypes.TypedData {
	// Build pieceData entries: [{data: bytes}, ...]
	pieceDataArr := make([]apitypes.TypedDataMessage, len(pieceCIDs))
	for i, c := range pieceCIDs {
		pieceDataArr[i] = apitypes.TypedDataMessage{
			"data": c.Bytes(),
		}
	}
	// Build pieceMetadata entries: [{pieceIndex: uint256, metadata: [{key, value}, ...]}, ...]
	pieceMetaArr := make([]apitypes.TypedDataMessage, len(pieceCIDs))
	for i := range pieceCIDs {
		entries := make([]apitypes.TypedDataMessage, len(metaKeys))
		for j := range metaKeys {
			entries[j] = apitypes.TypedDataMessage{
				"key":   metaKeys[j],
				"value": metaValues[j],
			}
		}
		pieceMetaArr[i] = apitypes.TypedDataMessage{
			"pieceIndex": big.NewInt(int64(i)).String(),
			"metadata":   entries,
		}
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
			"Cid": []apitypes.Type{
				{Name: "data", Type: "bytes"},
			},
			"PieceMetadata": []apitypes.Type{
				{Name: "pieceIndex", Type: "uint256"},
				{Name: "metadata", Type: "MetadataEntry[]"},
			},
			"AddPieces": []apitypes.Type{
				{Name: "clientDataSetId", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "pieceData", Type: "Cid[]"},
				{Name: "pieceMetadata", Type: "PieceMetadata[]"},
			},
		},
		PrimaryType: "AddPieces",
		Domain: apitypes.TypedDataDomain{
			Name:              "FilecoinWarmStorageService",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(int64(chainID)),
			VerifyingContract: fwss.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"clientDataSetId": clientDataSetID.String(),
			"nonce":           nonce.String(),
			"pieceData":       pieceDataArr,
			"pieceMetadata":   pieceMetaArr,
		},
	}
}

// lookupClientDataSetID calls
// FilecoinWarmStorageServiceStateView.getDataSet(dataSetId).clientDataSetId.
// The struct return shape (from the StateView ABI) is:
//
//	pdpRailId uint256        (offset 0)
//	cacheMissRailId uint256  (offset 32)
//	cdnRailId uint256        (offset 64)
//	payer address            (offset 96)
//	payee address            (offset 128)
//	serviceProvider address  (offset 160)
//	commissionBps uint256    (offset 192)
//	clientDataSetId uint256  (offset 224)
//	...
//
// We hit upstream glif directly for this read-only call; daemon
// proxying not needed.
func lookupClientDataSetID(ctx context.Context, rpcBase string, network string, dataSetID uint64) (*big.Int, error) {
	// Query a node RPC on the SAME network as the dataset. The previous
	// hardcoded calibration RPC + calibration StateView returned a wrong
	// clientDataSetId on mainnet, which made the AddPieces EIP-712 signature
	// recover to the wrong address (FWSS InvalidSignature revert).
	rpcURL := strings.TrimRight(rpcBase, "/") + "/rpc/v1"
	// FilecoinWarmStorageServiceStateView, per network.
	var stateView common.Address
	switch network {
	case "mainnet":
		stateView = common.HexToAddress("0xad28bbf18a72f728ed816d07f5a1d7ec40d68b5e")
	case "calibration":
		stateView = common.HexToAddress("0x537320bd004a7FDd3c1932ca64BD88268301322A")
	default:
		return nil, fmt.Errorf("lookupClientDataSetID: unknown network %q", network)
	}
	selector := crypto.Keccak256([]byte("getDataSet(uint256)"))[:4]
	arg := common.BigToHash(new(big.Int).SetUint64(dataSetID))
	data := append([]byte{}, selector...)
	data = append(data, arg.Bytes()...)
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_call",
		"params": []any{
			map[string]string{
				"to":   stateView.Hex(),
				"data": "0x" + hex.EncodeToString(data),
			},
			"latest",
		},
	}
	bj, _ := json.Marshal(body)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Post(rpcURL, "application/json", bytes.NewReader(bj))
	if err != nil {
		return nil, fmt.Errorf("eth_call: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var rpcResp struct {
		Result string `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("decode eth_call: %w (body: %s)", err, string(respBody))
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("eth_call returned error: %s", rpcResp.Error.Message)
	}
	raw := strings.TrimPrefix(rpcResp.Result, "0x")
	if len(raw) < 64*11 { // 11 fields of 32 bytes each
		return nil, fmt.Errorf("unexpected getDataSet result length %d", len(raw))
	}
	// clientDataSetId is at offset 224 (the 8th uint256, byte offset 224).
	// In hex string terms that's char offset 224*2 = 448, length 64.
	clientIDHex := raw[448:512]
	clientID, ok := new(big.Int).SetString(clientIDHex, 16)
	if !ok {
		return nil, fmt.Errorf("parse clientDataSetId: %q", clientIDHex)
	}
	return clientID, nil
}

// Pull randUint256 from cmd_demo.go (it's defined there); ensure import
// pulls in cryptorand if we add new uses later.
var _ = cryptorand.Reader
