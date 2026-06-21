package usdfcacquire

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// SourceChain bundles the RPC + numeric chain id for the chain the Squid
// source transaction executes on (Ethereum/Base/Arbitrum/...). curio-core
// already vaults Alchemy RPCs for these.
type SourceChain struct {
	ChainID int64
	RPCURL  string
}

// SignAndBroadcast signs the Squid transactionRequest with privKey (raw 32
// bytes, as returned by wallet.Export) and broadcasts it on src. Returns the
// source-chain tx hash. This is the SOURCE-chain leg; the cross-chain fill
// to USDFC on Filecoin is then tracked via Client.Status.
//
// Safety: this spends real funds on the source chain. Callers MUST gate it
// behind explicit operator confirmation (the CLI shows the quote + asks).
func SignAndBroadcast(ctx context.Context, src SourceChain, privKey []byte, tr TransactionRequest) (string, error) {
	if tr.RouteType != "" && tr.RouteType != "ON_CHAIN_EXECUTION" {
		return "", fmt.Errorf("unsupported Squid route type %q (only ON_CHAIN_EXECUTION can be signed locally)", tr.RouteType)
	}
	if tr.Target == "" || tr.Data == "" {
		return "", fmt.Errorf("squid transactionRequest missing target/data")
	}
	key, err := toECDSA(privKey)
	if err != nil {
		return "", err
	}
	from := crypto.PubkeyToAddress(key.PublicKey)

	cl, err := ethclient.DialContext(ctx, src.RPCURL)
	if err != nil {
		return "", fmt.Errorf("dial source rpc: %w", err)
	}
	defer cl.Close()

	chainID := big.NewInt(src.ChainID)
	to := common.HexToAddress(tr.Target)
	data := common.FromHex(tr.Data)
	value := parseBig(tr.Value)

	nonce, err := cl.PendingNonceAt(ctx, from)
	if err != nil {
		return "", fmt.Errorf("get nonce: %w", err)
	}

	// Prefer Squid's recommended fees; fall back to live suggestions.
	gasTip := parseBigOpt(tr.MaxPriorityFeePerGas)
	gasFeeCap := parseBigOpt(tr.MaxFeePerGas)
	if gasTip == nil {
		gasTip, err = cl.SuggestGasTipCap(ctx)
		if err != nil {
			return "", fmt.Errorf("suggest gas tip: %w", err)
		}
	}
	if gasFeeCap == nil {
		head, herr := cl.HeaderByNumber(ctx, nil)
		if herr != nil {
			return "", fmt.Errorf("header: %w", herr)
		}
		base := new(big.Int)
		if head.BaseFee != nil {
			base = head.BaseFee
		}
		gasFeeCap = new(big.Int).Add(new(big.Int).Mul(base, big.NewInt(2)), gasTip)
	}

	gasLimit := parseUint(tr.GasLimit)
	if gasLimit == 0 {
		est, eerr := cl.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, Value: value, Data: data})
		if eerr != nil {
			return "", fmt.Errorf("estimate gas: %w", eerr)
		}
		gasLimit = est + est/4 // 25% headroom
	}

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasTip,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
		Data:      data,
	})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), key)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}
	if err := cl.SendTransaction(ctx, signed); err != nil {
		return "", fmt.Errorf("broadcast tx: %w", err)
	}
	return signed.Hash().Hex(), nil
}

// WaitFilled polls Squid status until the cross-chain fill reaches a terminal
// state or ctx is done. Returns the final status string.
func (c *Client) WaitFilled(ctx context.Context, srcTxHash, fromChainID, toChainID, quoteID string, poll time.Duration) (string, error) {
	if poll <= 0 {
		poll = 15 * time.Second
	}
	for {
		st, err := c.Status(ctx, srcTxHash, fromChainID, toChainID, quoteID)
		if err == nil {
			switch strings.ToLower(st.Status) {
			case "success", "partial_success":
				return st.Status, nil
			case "needs_gas", "not_found":
				// transient early; keep polling a bounded number of times
				// via ctx deadline rather than failing immediately.
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(poll):
		}
	}
}

func toECDSA(raw []byte) (*ecdsa.PrivateKey, error) {
	if len(raw) != 32 {
		return nil, fmt.Errorf("private key must be 32 bytes, got %d", len(raw))
	}
	return crypto.ToECDSA(raw)
}

func parseBig(s string) *big.Int {
	v := parseBigOpt(s)
	if v == nil {
		return big.NewInt(0)
	}
	return v
}

func parseBigOpt(s string) *big.Int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if b, err := hexutil.DecodeBig(s); err == nil {
			return b
		}
		return nil
	}
	if b, ok := new(big.Int).SetString(s, 10); ok {
		return b
	}
	return nil
}

func parseUint(s string) uint64 {
	b := parseBigOpt(s)
	if b == nil {
		return 0
	}
	return b.Uint64()
}
