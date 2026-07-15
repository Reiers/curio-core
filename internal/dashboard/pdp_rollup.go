package dashboard

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
)

// epochsPerDay is the Filecoin cadence: 30s epochs => 2880/day. Used to
// project a per-epoch USDFC rail rate into daily / 30-day income.
const epochsPerDay = 2880

// finRollupTTL bounds how often we walk the rails on-chain. The overview
// auto-refreshes every 30s; without this cache each refresh would fan out
// one getRail call per active rail. 45s keeps the panel live without
// hammering the embedded node.
const finRollupTTL = 45 * time.Second

// finRollup is a cached, finance-focused summary of incoming PDP income.
// It is projection-only (rate * time), NOT settled/realized revenue —
// the honest framing the datasets/messages pages already use.
type finRollup struct {
	ActiveRails  int
	RatePerEpoch string // USDFC/epoch, 18-decimal display
	RatePerDay   string // USDFC/day
	RatePer30d   string // USDFC/30d
	AsOf         time.Time
	Fresh        bool
	Unavailable  bool // eth client / pay contract unwired
}

// finRollupCache is a TTL cache with singleflight-style single-owner
// recompute. A zero value is not usable; construct via newFinRollupCache.
type finRollupCache struct {
	mu       sync.Mutex
	val      finRollup
	computed time.Time
	running  bool
	now      func() time.Time
}

func newFinRollupCache() *finRollupCache { return &finRollupCache{} }

func (c *finRollupCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// get returns the cached rollup when fresh, otherwise recomputes inline.
// Only one caller recomputes at a time; while a recompute is in flight,
// other callers get the previous (stale-flagged) value immediately.
func (c *finRollupCache) get(ctx context.Context, compute func(context.Context) finRollup) finRollup {
	c.mu.Lock()
	fresh := c.clock().Sub(c.computed) < finRollupTTL && !c.computed.IsZero()
	if fresh {
		v := c.val
		v.Fresh = true
		c.mu.Unlock()
		return v
	}
	if c.running {
		v := c.val
		v.Fresh = false
		c.mu.Unlock()
		return v
	}
	c.running = true
	c.mu.Unlock()

	v := compute(ctx)

	c.mu.Lock()
	c.val = v
	c.computed = c.clock()
	c.running = false
	v.Fresh = true
	c.mu.Unlock()
	return v
}

// computeFinRollup walks non-terminated rails, sums their on-chain
// paymentRate, and projects daily / 30-day income. Best-effort: any
// per-rail failure is skipped; a missing eth client marks the rollup
// Unavailable so the UI hides the panel instead of showing zeros.
func (s *Server) computeFinRollup(ctx context.Context) finRollup {
	if s.eth == nil || s.payAddr == (common.Address{}) {
		return finRollup{Unavailable: true}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var railIDs []struct {
		RailID int64 `db:"rail_id"`
	}
	if err := s.db.SelectI(ctx, &railIDs,
		`SELECT rail_id FROM pdp_payment_rails WHERE terminated=0 ORDER BY rail_id`); err != nil {
		return finRollup{Unavailable: true}
	}

	pay, err := filecoinpayment.NewPayments(s.payAddr, s.eth)
	if err != nil {
		return finRollup{Unavailable: true}
	}

	total := new(big.Int)
	active := 0
	for _, r := range railIDs {
		view, gErr := pay.GetRail(&bind.CallOpts{Context: ctx}, big.NewInt(r.RailID))
		if gErr != nil || view.PaymentRate == nil {
			continue
		}
		total.Add(total, view.PaymentRate)
		active++
	}

	perDay := new(big.Int).Mul(total, big.NewInt(epochsPerDay))
	per30d := new(big.Int).Mul(perDay, big.NewInt(30))

	return finRollup{
		ActiveRails:  active,
		RatePerEpoch: decimal18(total),
		RatePerDay:   decimal18(perDay),
		RatePer30d:   decimal18(per30d),
	}
}
