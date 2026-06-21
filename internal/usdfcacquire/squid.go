// Package usdfcacquire implements headless USDFC acquisition for a Hot
// Storage SP (curio-core#92). Datasets are paid in USDFC, not FIL, and
// there is no deep native FIL->USDFC liquidity on Filecoin -- the realistic
// way to obtain USDFC is to bridge a dollar stablecoin (USDC) in from
// another chain. Secured Finance's own app does this via Squid Router.
//
// This package lets curio-core do the same WITHOUT a browser wallet: it
// quotes a Squid route (source-chain USDC -> Filecoin USDFC), signs the
// returned source-chain transaction with the operator's own EVM key, and
// broadcasts + tracks it to completion. The result lands USDFC directly in
// the SP's PDP wallet on Filecoin -- fully headless, no browser wallet
// required.
//
// Squid contract: POST /v2/route returns a transactionRequest{target,data,
// value,gasLimit}; GET /v2/status tracks the cross-chain fill. Both require
// the x-integrator-id header (self-serve ID via Squid's Typeform).
//
// This file is the HTTP client + types only. Signing/broadcast lives in
// signer.go; CLI dispatch in cmd/curio-core. No chain calls here = fully
// unit-testable against a stub server with no integrator ID.
package usdfcacquire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is Squid's v2 API host.
const DefaultBaseURL = "https://v2.api.squidrouter.com"

// Filecoin mainnet identifiers (durable).
const (
	FilecoinChainID = "314"
	USDFCFilecoin   = "0x80B98d3aa09ffff255c3ba4A241111Ff1262F045"
	defaultTimeout  = 30 * time.Second
	defaultSlippage = 1.5 // percent
)

// Client talks to the Squid v2 API. Construct via NewClient.
type Client struct {
	baseURL      string
	integratorID string
	http         *http.Client
}

// NewClient builds a Squid client. integratorID must be a valid Squid
// integrator id (x-integrator-id header); the API rejects empty/invalid ids
// with 401 UNAUTHORIZED. baseURL empty -> DefaultBaseURL.
func NewClient(integratorID, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		integratorID: integratorID,
		http:         &http.Client{Timeout: defaultTimeout},
	}
}

// HasIntegratorID reports whether an id is configured. Callers should fail
// fast with an actionable message when false rather than hit a 401.
func (c *Client) HasIntegratorID() bool { return strings.TrimSpace(c.integratorID) != "" }

// RouteParams describes a desired acquisition: bring `FromToken` on
// `FromChain` to USDFC on Filecoin, delivered to `ToAddress`.
type RouteParams struct {
	FromChain   string // e.g. "1" (Ethereum), "8453" (Base), "42161" (Arbitrum)
	FromToken   string // source stablecoin (USDC) contract on FromChain
	FromAmount  string // smallest-unit amount as decimal string
	FromAddress string // sender on the source chain (our key's address)
	ToAddress   string // receiver on Filecoin (the PDP wallet)
	ToToken     string // empty -> USDFC on Filecoin
	ToChain     string // empty -> Filecoin "314"
	Slippage    float64
	QuoteOnly   bool // true: no transactionRequest (quote only, no signing)
}

// TransactionRequest is the EVM ON_CHAIN_EXECUTION payload to sign+broadcast.
type TransactionRequest struct {
	RouteType            string `json:"type"`
	Target               string `json:"target"`
	Data                 string `json:"data"`
	Value                string `json:"value"`
	GasLimit             string `json:"gasLimit"`
	GasPrice             string `json:"gasPrice"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
}

// RouteEstimate is the human-facing part of a quote.
type RouteEstimate struct {
	ToAmount             string `json:"toAmount"`
	ToAmountMin          string `json:"toAmountMin"`
	FromAmount           string `json:"fromAmount"`
	ExchangeRate         string `json:"exchangeRate"`
	AggregatePriceImpact string `json:"aggregatePriceImpact"`
}

// RouteResponse is the subset of POST /v2/route we consume.
type RouteResponse struct {
	Route struct {
		Estimate           RouteEstimate      `json:"estimate"`
		TransactionRequest TransactionRequest `json:"transactionRequest"`
	} `json:"route"`
	QuoteID string `json:"quoteId"`
}

// Route requests a route. With QuoteOnly=false the response carries a
// signable transactionRequest. Returns a typed error on non-2xx.
func (c *Client) Route(ctx context.Context, p RouteParams) (*RouteResponse, error) {
	if !c.HasIntegratorID() {
		return nil, ErrNoIntegratorID
	}
	if p.ToToken == "" {
		p.ToToken = USDFCFilecoin
	}
	if p.ToChain == "" {
		p.ToChain = FilecoinChainID
	}
	if p.Slippage == 0 {
		p.Slippage = defaultSlippage
	}
	body := map[string]any{
		"fromChain":   p.FromChain,
		"fromToken":   p.FromToken,
		"fromAmount":  p.FromAmount,
		"fromAddress": p.FromAddress,
		"toChain":     p.ToChain,
		"toToken":     p.ToToken,
		"toAddress":   p.ToAddress,
		"slippage":    p.Slippage,
		"quoteOnly":   p.QuoteOnly,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v2/route", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-integrator-id", c.integratorID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("squid route request: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNoIntegratorID
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("squid route HTTP %d: %s", resp.StatusCode, truncate(string(rb), 300))
	}
	var out RouteResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, fmt.Errorf("decode squid route: %w (body: %s)", err, truncate(string(rb), 200))
	}
	return &out, nil
}

// StatusResult is the subset of GET /v2/status we surface.
type StatusResult struct {
	Status      string         `json:"status"` // e.g. ongoing, success, partial_success, needs_gas, not_found
	SquidTxHash string         `json:"squidTransactionStatus"`
	Raw         map[string]any `json:"-"`
}

// Status polls a cross-chain fill. transactionId is the SOURCE-chain tx hash.
func (c *Client) Status(ctx context.Context, transactionId, fromChainID, toChainID, quoteID string) (*StatusResult, error) {
	if !c.HasIntegratorID() {
		return nil, ErrNoIntegratorID
	}
	q := fmt.Sprintf("%s/v2/status?transactionId=%s&fromChainId=%s&toChainId=%s&quoteId=%s",
		c.baseURL, transactionId, fromChainID, toChainID, quoteID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-integrator-id", c.integratorID)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("squid status request: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("squid status HTTP %d: %s", resp.StatusCode, truncate(string(rb), 300))
	}
	var m map[string]any
	_ = json.Unmarshal(rb, &m)
	out := &StatusResult{Raw: m}
	if s, ok := m["status"].(string); ok {
		out.Status = s
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ErrNoIntegratorID is returned when no valid Squid integrator id is set.
var ErrNoIntegratorID = fmt.Errorf(
	"no Squid integrator id configured: apply at https://squidrouter.typeform.com/integrator-id " +
		"and set CURIO_SQUID_INTEGRATOR_ID (or .vault/squid.md)")
