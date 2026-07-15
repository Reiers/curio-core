package dashboard

import (
	"context"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseCoinbaseSpot(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    float64
		wantErr bool
	}{
		{"ok", `{"data":{"amount":"3.2145","currency":"USD"}}`, 3.2145, false},
		{"integer", `{"data":{"amount":"5","currency":"USD"}}`, 5, false},
		{"empty amount", `{"data":{"amount":"","currency":"USD"}}`, 0, true},
		{"zero", `{"data":{"amount":"0","currency":"USD"}}`, 0, true},
		{"negative", `{"data":{"amount":"-1","currency":"USD"}}`, 0, true},
		{"garbage", `not json`, 0, true},
		{"bad number", `{"data":{"amount":"abc"}}`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCoinbaseSpot([]byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestUsdFromWei(t *testing.T) {
	oneFIL := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if got := usdFromWei(oneFIL, 4.0); got != 4.0 {
		t.Fatalf("1 FIL @ $4 => %v, want 4", got)
	}
	half := new(big.Int).Div(oneFIL, big.NewInt(2))
	if got := usdFromWei(half, 4.0); got != 2.0 {
		t.Fatalf("0.5 FIL @ $4 => %v, want 2", got)
	}
	if got := usdFromWei(nil, 4.0); got != 0 {
		t.Fatalf("nil wei => %v, want 0", got)
	}
	if got := usdFromWei(oneFIL, 0); got != 0 {
		t.Fatalf("zero rate => %v, want 0", got)
	}
}

func TestPriceCacheFreshAndTTL(t *testing.T) {
	var calls int64
	now := time.Now()
	p := newPriceCache()
	p.now = func() time.Time { return now }
	p.fetch = func(ctx context.Context) (float64, error) {
		atomic.AddInt64(&calls, 1)
		return 3.50, nil
	}

	// First Get: miss -> fetch.
	got := p.Get(context.Background())
	if !got.Fresh || got.USD != 3.50 {
		t.Fatalf("first get: %+v", got)
	}
	if atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("expected 1 fetch, got %d", calls)
	}

	// Second Get within TTL: cache hit, no new fetch.
	got = p.Get(context.Background())
	if !got.Fresh || got.USD != 3.50 || atomic.LoadInt64(&calls) != 1 {
		t.Fatalf("cache hit failed: %+v calls=%d", got, calls)
	}

	// Advance past TTL: refetch.
	now = now.Add(filPriceTTL + time.Second)
	got = p.Get(context.Background())
	if atomic.LoadInt64(&calls) != 2 {
		t.Fatalf("expected refetch after TTL, calls=%d", calls)
	}
	if !got.Fresh {
		t.Fatalf("post-ttl refetch should be fresh: %+v", got)
	}
}

func TestPriceCacheErrorKeepsLastGood(t *testing.T) {
	var fail atomic.Bool
	now := time.Now()
	p := newPriceCache()
	p.now = func() time.Time { return now }
	p.fetch = func(ctx context.Context) (float64, error) {
		if fail.Load() {
			return 0, context.DeadlineExceeded
		}
		return 2.0, nil
	}

	got := p.Get(context.Background())
	if got.USD != 2.0 || !got.Fresh {
		t.Fatalf("seed: %+v", got)
	}

	// Force TTL expiry + failing fetch: last good value returned, stale.
	now = now.Add(filPriceTTL + time.Second)
	fail.Store(true)
	got = p.Get(context.Background())
	if got.USD != 2.0 {
		t.Fatalf("should keep last good value: %+v", got)
	}
	if got.Fresh {
		t.Fatalf("stale value should not be marked fresh: %+v", got)
	}
	if got.Err == "" {
		t.Fatalf("error should be surfaced: %+v", got)
	}
}

func TestPriceCacheNeverFetchedError(t *testing.T) {
	p := newPriceCache()
	p.fetch = func(ctx context.Context) (float64, error) {
		return 0, context.DeadlineExceeded
	}
	got := p.Get(context.Background())
	if got.Fresh || got.USD != 0 || got.Err == "" {
		t.Fatalf("cold failure should be non-fresh empty with err: %+v", got)
	}
}

func TestPriceCacheSingleflightCoalesces(t *testing.T) {
	var calls int64
	release := make(chan struct{})
	p := newPriceCache()
	p.fetch = func(ctx context.Context) (float64, error) {
		atomic.AddInt64(&calls, 1)
		<-release // hold so concurrent callers pile onto the same flight
		return 1.23, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.Get(context.Background()) }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if c := atomic.LoadInt64(&calls); c != 1 {
		t.Fatalf("singleflight should coalesce to 1 fetch, got %d", c)
	}
}
