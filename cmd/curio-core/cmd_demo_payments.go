// cmd_demo_payments.go - FilecoinPay client-side setup for the P5 demo flow.
//
// Before a synapse-sdk-shaped createDataSet can land on-chain, the client
// (payer) wallet needs three preparations on the FilecoinPay V1 contract:
//
//   1. USDFC.approve(FilecoinPay, amount)       -- ERC-20 approve
//   2. FilecoinPay.deposit(USDFC, self, amount) -- move USDFC into FilecoinPay
//   3. FilecoinPay.setOperatorApproval(USDFC, FWSS, true, rateAllowance,
//                                       lockupAllowance, maxLockupPeriod)
//                                               -- let FWSS pull from us
//
// Without these, the FWSS.dataSetCreated() callback reverts with
// InsufficientLockupFunds(payer, minimumRequired=0.16 USDFC, available=0).
//
// This subcommand drives all three transactions through the running daemon's
// /admin/test-tx endpoint, which builds a SenderETH harmonytask per tx; the
// scheduler claims them and broadcasts via embedded Lantern's VMBridge.
//
// Defaults follow synapse-sdk's pay/set-operator-approval.ts:
//   - rateAllowance:   maxUint256
//   - lockupAllowance: maxUint256
//   - maxLockupPeriod: 30 * 2880 = 86400 epochs (30 days on Filecoin)
//
// Reference contracts (calibration):
//   USDFC:        0xb3042734b608a1B16e9e86B374A3f3e389B4cDf0
//   FilecoinPay:  0x09a0fDc2723fAd1A7b8e3e00eE5DF73841df55a0
//   FWSS proxy:   0x02925630df557F957f70E112bA06e50965417CA0

package main

import (
	"bytes"
	"context"
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
	ethCrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
)

// ethCryptoKeccak256 is the actual function reference. cryptoKeccak256
// below provides a redirectable alias for testing.
var ethCryptoKeccak256 = ethCrypto.Keccak256

// Default operator-approval parameters mirror synapse-sdk defaults
// (packages/synapse-core/src/pay/set-operator-approval.ts +
//
//	packages/synapse-core/src/utils/constants.ts: LOCKUP_PERIOD =
//	DEFAULT_LOCKUP_DAYS(30) * EPOCHS_PER_DAY(2880) = 86400).
const (
	defaultMaxLockupPeriodEpochs = 86400 // 30 days * 2880 epochs/day
)

// maxUint256 returns 2^256 - 1 as *big.Int.
func maxUint256() *big.Int {
	v := new(big.Int).Lsh(big.NewInt(1), 256)
	return v.Sub(v, big.NewInt(1))
}

// ABI argument bundles for the three calls. We hand-roll these instead
// of importing the upstream generated bindings to keep the demo CLI
// build graph small.

var (
	erc20ApproveArgs = abi.Arguments{
		{Type: mustABIType("address")}, // spender
		{Type: mustABIType("uint256")}, // amount
	}

	filecoinPayDepositArgs = abi.Arguments{
		{Type: mustABIType("address")}, // token
		{Type: mustABIType("address")}, // to
		{Type: mustABIType("uint256")}, // amount
	}

	filecoinPaySetOperatorApprovalArgs = abi.Arguments{
		{Type: mustABIType("address")}, // token
		{Type: mustABIType("address")}, // operator
		{Type: mustABIType("bool")},    // approved
		{Type: mustABIType("uint256")}, // rateAllowance
		{Type: mustABIType("uint256")}, // lockupAllowance
		{Type: mustABIType("uint256")}, // maxLockupPeriod
	}
)

// Function selectors (4-byte keccak256 of the canonical signature).
// Pre-computed once at package init; verified against cast keccak in
// the commit message.
var (
	selErc20Approve              = mustSelector("approve(address,uint256)")
	selFilecoinPayDeposit        = mustSelector("deposit(address,address,uint256)")
	selFilecoinPaySetOperatorApp = mustSelector("setOperatorApproval(address,address,bool,uint256,uint256,uint256)")
)

func mustSelector(sig string) []byte {
	return cryptoKeccak256([]byte(sig))[:4]
}

func cmdDemoPrepareClientPayments(args []string) error {
	fs := flag.NewFlagSet("demo prepare-client-payments", flag.ContinueOnError)
	dataDir := fs.String("data-dir", defaultDataDir(), "curio-core data directory")
	network := fs.String("network", "calibration", "network (calibration|mainnet)")
	daemon := fs.String("daemon", "http://127.0.0.1:14994", "daemon base URL (/admin/test-tx will be appended)")
	depositAmount := fs.String("deposit", "1", "USDFC amount to deposit (decimal USDFC, default 1 USDFC = 1e18 base units)")
	dryRun := fs.Bool("dry-run", false, "print all three tx payloads without submitting")
	submit := fs.Bool("submit", false, "POST all three txes to the daemon's /admin/test-tx endpoint")
	skipApprove := fs.Bool("skip-approve", false, "skip USDFC.approve (use if already approved)")
	skipDeposit := fs.Bool("skip-deposit", false, "skip FilecoinPay.deposit (use if already deposited)")
	skipOperator := fs.Bool("skip-operator", false, "skip setOperatorApproval (use if already approved)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *submit {
		return errors.New("specify exactly one of --dry-run or --submit")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Resolve the daemon's pdp wallet — that's our payer.
	db, closeDB, err := openWalletDB(ctx, *dataDir)
	if err != nil {
		return fmt.Errorf("open wallet DB: %w", err)
	}
	var payerHex string
	if err := db.QueryRowI(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&payerHex); err != nil {
		closeDB()
		return fmt.Errorf("read pdp wallet address: %w", err)
	}
	closeDB()
	payer := common.HexToAddress(payerHex)

	// Resolve network-dependent contract addresses.
	nw := contract.Network(*network)
	usdfc, err := contract.USDFCAddressFor(nw)
	if err != nil {
		return fmt.Errorf("USDFC address: %w", err)
	}
	pay, err := filecoinpayment.PaymentContractAddressFor(nw)
	if err != nil {
		return fmt.Errorf("FilecoinPay address: %w", err)
	}
	fwss := contract.ContractAddressesFor(nw).AllowedPublicRecordKeepers.FWSService

	// Parse deposit amount: decimal USDFC -> wei (10^18 base units).
	depWei, err := parseUSDFC(*depositAmount)
	if err != nil {
		return fmt.Errorf("parse --deposit: %w", err)
	}

	fmt.Println("=== prepare-client-payments ===")
	fmt.Printf("  network:           %s\n", nw)
	fmt.Printf("  payer (= signer):  %s\n", payer.Hex())
	fmt.Printf("  USDFC token:       %s\n", usdfc.Hex())
	fmt.Printf("  FilecoinPay V1:    %s\n", pay.Hex())
	fmt.Printf("  FWSS proxy:        %s\n", fwss.Hex())
	fmt.Printf("  deposit amount:    %s USDFC (%s base units)\n", *depositAmount, depWei.String())
	fmt.Printf("  rate allowance:    maxUint256 (synapse-sdk default)\n")
	fmt.Printf("  lockup allowance:  maxUint256 (synapse-sdk default)\n")
	fmt.Printf("  max lockup period: %d epochs (~30 days)\n", defaultMaxLockupPeriodEpochs)
	fmt.Println()

	// Build the three txes.
	type plannedTx struct {
		name  string
		skip  bool
		to    common.Address
		value *big.Int
		data  []byte
	}
	approveAmount := maxUint256() // approve max so we don't need to re-approve later
	approveData, err := encodeCall(selErc20Approve, erc20ApproveArgs, pay, approveAmount)
	if err != nil {
		return fmt.Errorf("encode approve: %w", err)
	}
	depositData, err := encodeCall(selFilecoinPayDeposit, filecoinPayDepositArgs, usdfc, payer, depWei)
	if err != nil {
		return fmt.Errorf("encode deposit: %w", err)
	}
	opData, err := encodeCall(selFilecoinPaySetOperatorApp, filecoinPaySetOperatorApprovalArgs,
		usdfc, fwss, true, maxUint256(), maxUint256(), big.NewInt(defaultMaxLockupPeriodEpochs))
	if err != nil {
		return fmt.Errorf("encode setOperatorApproval: %w", err)
	}

	plan := []plannedTx{
		{name: "USDFC.approve(FilecoinPay, max)", skip: *skipApprove, to: usdfc, value: big.NewInt(0), data: approveData},
		{name: fmt.Sprintf("FilecoinPay.deposit(USDFC, self, %s)", *depositAmount), skip: *skipDeposit, to: pay, value: big.NewInt(0), data: depositData},
		{name: "FilecoinPay.setOperatorApproval(USDFC, FWSS, true, max, max, 86400)", skip: *skipOperator, to: pay, value: big.NewInt(0), data: opData},
	}

	for i, t := range plan {
		fmt.Printf("[%d/3] %s\n", i+1, t.name)
		if t.skip {
			fmt.Println("       (skipped by flag)")
			continue
		}
		fmt.Printf("       to:    %s\n", t.to.Hex())
		fmt.Printf("       value: %s wei\n", t.value.String())
		fmt.Printf("       data:  0x%x (%d bytes)\n", t.data, len(t.data))
		if *dryRun {
			continue
		}
		// --submit: POST to /admin/test-tx.
		txHash, err := submitViaAdminTestTx(ctx, *daemon, t.to, t.value, t.data)
		if err != nil {
			return fmt.Errorf("[%d/3] submit %s: %w", i+1, t.name, err)
		}
		fmt.Printf("       txHash: %s\n", txHash)
		fmt.Printf("       watch:  https://calibration.filfox.info/en/message/%s\n", txHash)
		// Give the chain a few seconds to absorb before the next one
		// (each tx changes state the next one depends on).
		if i < len(plan)-1 {
			time.Sleep(5 * time.Second)
		}
	}

	if *submit {
		fmt.Println()
		fmt.Println("All three txes broadcast. Wait ~60-90s for confirmations, then re-run")
		fmt.Println("'curio-core demo create-dataset --submit' — should now clear the FWSS")
		fmt.Println("InsufficientLockupFunds gate.")
	}
	return nil
}

// submitViaAdminTestTx POSTs {to, value, data} to the daemon's
// /admin/test-tx endpoint. Returns the broadcasted tx hash.
func submitViaAdminTestTx(ctx context.Context, daemon string, to common.Address, value *big.Int, data []byte) (string, error) {
	body := map[string]string{
		"to":    to.Hex(),
		"value": "0x" + value.Text(16),
		"data":  fmt.Sprintf("0x%x", data),
	}
	bj, _ := json.Marshal(body)
	url := strings.TrimRight(daemon, "/") + "/admin/test-tx"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bj))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("daemon HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	var r struct {
		TxHash string `json:"txHash"`
		From   string `json:"from"`
	}
	if err := json.Unmarshal(respBody, &r); err != nil {
		return "", fmt.Errorf("decode response: %w (body: %s)", err, string(respBody))
	}
	if r.TxHash == "" {
		return "", fmt.Errorf("daemon returned empty txHash (body: %s)", string(respBody))
	}
	return r.TxHash, nil
}

// encodeCall builds the calldata for an Ethereum function call:
// selector(4 bytes) || abi-encoded(args...).
func encodeCall(selector []byte, args abi.Arguments, vals ...interface{}) ([]byte, error) {
	packed, err := args.Pack(vals...)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 4+len(packed))
	out = append(out, selector...)
	out = append(out, packed...)
	return out, nil
}

// parseUSDFC parses a decimal USDFC amount like "1" or "0.5" into wei
// (10^18 base units). Tolerates an optional leading whitespace and the
// "USDFC" suffix.
func parseUSDFC(s string) (*big.Int, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "USDFC"))
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		// integer USDFC -> multiply by 10^18
		n, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, fmt.Errorf("not a decimal number: %q", s)
		}
		return n.Mul(n, big.NewInt(1).Exp(big.NewInt(10), big.NewInt(18), nil)), nil
	}
	whole := s[:dot]
	frac := s[dot+1:]
	if len(frac) > 18 {
		return nil, fmt.Errorf("too many decimal places (USDFC has 18 decimals): %q", s)
	}
	// pad fraction to 18 decimals
	frac = frac + strings.Repeat("0", 18-len(frac))
	combined := whole + frac
	if combined == "" || combined == "0" {
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(combined, 10)
	if !ok {
		return nil, fmt.Errorf("not a decimal number: %q", s)
	}
	return n, nil
}

// cryptoKeccak256 is a thin alias over go-ethereum's crypto.Keccak256.
// Defined as a package-level var so the imports stay tidy: this file
// only pulls in "github.com/ethereum/go-ethereum/crypto" via this var.
var cryptoKeccak256 = ethCryptoKeccak256
