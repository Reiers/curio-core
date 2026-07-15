package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// filPriceTTL is how long a fetched FIL/USD rate is considered fresh.
// The rate feeds cosmetic USD annotations only (wallet balances, rail
// projections); it is never used for anything that moves money, so a
// coarse TTL is fine and keeps us off the price API.
const filPriceTTL = 5 * time.Minute

// filPriceSource is a keyless public spot endpoint. It is best-effort:
// if it is unreachable (airgapped box, rate-limit, DNS) the dashboard
// simply renders FIL values without a USD annotation. We never block a
// page render on it.
const filPriceSource = "https://api.coinbase.com/v2/prices/FIL-USD/spot"

// priceCache is a tiny TTL cache with singleflight de-duplication so a
// burst of concurrent page loads collapses into a single upstream fetch.
// A zero value is ready to use.
type priceCache struct {
	group singleflight.Group

	mu       sync.RWMutex
	usd      float64
	fetched  time.Time
	lastErr  string
	hasValue bool

	// now + fetch are overridable for tests.
	now   func() time.Time
	fetch func(ctx context.Context) (float64, error)
}

func newPriceCache() *priceCache {
	return &priceCache{}
}

func (p *priceCache) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *priceCache) fetcher() func(ctx context.Context) (float64, error) {
	if p.fetch != nil {
		return p.fetch
	}
	return fetchFILUSD
}

// filPrice is the snapshot handed to templates. Fresh is false when we
// have no usable rate (never fetched, or last fetch failed with no prior
// value); callers should hide the USD column in that case.
type filPrice struct {
	USD   float64
	Fresh bool
	AsOf  time.Time
	Err   string
}

// Get returns a cached rate when fresh, otherwise triggers a single
// coalesced refresh. It never blocks longer than the caller's context;
// on any error it returns the last good value (marked stale) or an
// empty, non-fresh snapshot.
func (p *priceCache) Get(ctx context.Context) filPrice {
	p.mu.RLock()
	age := p.clock().Sub(p.fetched)
	if p.hasValue && age < filPriceTTL {
		snap := filPrice{USD: p.usd, Fresh: true, AsOf: p.fetched}
		p.mu.RUnlock()
		return snap
	}
	p.mu.RUnlock()

	// Coalesce concurrent refreshes. The shared result is ignored; we
	// read back through the mutex so every caller sees the same state.
	_, _, _ = p.group.Do("fil-usd", func() (any, error) {
		v, err := p.fetcher()(ctx)
		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			p.lastErr = err.Error()
			return nil, nil
		}
		p.usd = v
		p.fetched = p.clock()
		p.hasValue = true
		p.lastErr = ""
		return nil, nil
	})

	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.hasValue {
		return filPrice{Err: p.lastErr}
	}
	fresh := p.clock().Sub(p.fetched) < filPriceTTL
	return filPrice{USD: p.usd, Fresh: fresh, AsOf: p.fetched, Err: p.lastErr}
}

// fetchFILUSD hits the public spot endpoint and parses the decimal
// dollar rate. Short timeout; any transport/shape error is returned so
// the cache keeps the previous value.
func fetchFILUSD(ctx context.Context) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, filPriceSource, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fil price: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return 0, err
	}
	return parseCoinbaseSpot(body)
}

// parseCoinbaseSpot decodes {"data":{"amount":"3.21","currency":"USD"}}.
// Split out for testability.
func parseCoinbaseSpot(body []byte) (float64, error) {
	var payload struct {
		Data struct {
			Amount string `json:"amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, err
	}
	if payload.Data.Amount == "" {
		return 0, fmt.Errorf("fil price: empty amount")
	}
	v, err := strconv.ParseFloat(payload.Data.Amount, 64)
	if err != nil {
		return 0, fmt.Errorf("fil price: parse amount %q: %w", payload.Data.Amount, err)
	}
	if v <= 0 {
		return 0, fmt.Errorf("fil price: non-positive amount %v", v)
	}
	return v, nil
}

// usdMoney formats a USD float for display: "$1,234.56", "$0.42", or
// "<$0.01" for tiny non-zero values so a dusty wallet does not read $0.00.
func usdMoney(v float64) string {
	if v <= 0 {
		return ""
	}
	if v < 0.01 {
		return "<$0.01"
	}
	cents := int64(v*100 + 0.5)
	whole := cents / 100
	frac := cents % 100
	// group thousands with commas
	s := strconv.FormatInt(whole, 10)
	var grouped []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			grouped = append(grouped, ',')
		}
		grouped = append(grouped, c)
	}
	return fmt.Sprintf("$%s.%02d", string(grouped), frac)
}

// usdFromWei converts an 18-decimal FIL/USDFC wei value to a USD float
// using the given per-token USD rate. Returns 0 for nil / non-positive
// rate so callers can cheaply decide whether to render the annotation.
func usdFromWei(wei *big.Int, ratePerToken float64) float64 {
	if wei == nil || wei.Sign() == 0 || ratePerToken <= 0 {
		return 0
	}
	// tokens = wei / 1e18, done in big.Float to avoid int overflow.
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(1e18))
	tokens, _ := f.Float64()
	return tokens * ratePerToken
}
