package dashboard

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// readyState is the tri-state a checklist row can report. Unknown means
// we could not verify (e.g. eth client unwired) — distinct from a hard
// "not ready", so the UI does not cry wolf on an airgapped box.
type readyState string

const (
	readyOK      readyState = "ok"
	readyWarn    readyState = "warn"
	readyFail    readyState = "fail"
	readyUnknown readyState = "unknown"
)

// readinessItem is one server-verified prerequisite for a healthy PDP SP.
type readinessItem struct {
	Key      string
	Label    string
	State    readyState
	Detail   string
	Critical bool // gates the overall "ready" verdict
}

// readinessReport is the rolled-up checklist plus an overall verdict.
type readinessReport struct {
	Items    []readinessItem
	OK       int // count of ok items
	Total    int
	AllReady bool // every CRITICAL item is ok
}

// computeReadiness builds the server-verified checklist. It reuses the
// already-collected overview snapshot for chain/dataset/proving signals
// and does one extra balance read for the PDP wallet. Every check
// degrades to "unknown" rather than failing the page.
func (s *Server) computeReadiness(ctx context.Context, ov overviewData) readinessReport {
	var items []readinessItem

	// 1. Chain reachable via the embedded Lantern node.
	chainState, chainDetail := readyFail, "embedded node not answering"
	switch {
	case ov.Chain.Reachable && ov.Chain.Synced:
		chainState, chainDetail = readyOK, "embedded Lantern reachable + synced"
	case ov.Chain.Reachable:
		chainState, chainDetail = readyWarn, "reachable, head not advancing yet"
	}
	items = append(items, readinessItem{
		Key: "chain", Label: "Chain node reachable", State: chainState,
		Detail: chainDetail, Critical: true,
	})

	// 2. PDP wallet configured.
	if s.cfg.PayeeAddress == "" {
		items = append(items, readinessItem{
			Key: "wallet", Label: "PDP wallet configured", State: readyFail,
			Detail: "no eth_keys role=pdp wallet set", Critical: true,
		})
	} else {
		items = append(items, readinessItem{
			Key: "wallet", Label: "PDP wallet configured", State: readyOK,
			Detail: s.cfg.PayeeAddress, Critical: true,
		})

		// 3. PDP wallet funded (only meaningful when configured).
		fundState, fundDetail := readyUnknown, "balance unavailable (eth client unwired)"
		if s.eth != nil && common.IsHexAddress(s.cfg.PayeeAddress) {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			bal, err := s.eth.BalanceAt(cctx, common.HexToAddress(s.cfg.PayeeAddress), nil)
			cancel()
			switch {
			case err != nil:
				fundState, fundDetail = readyUnknown, "balance read failed"
			case bal != nil && bal.Sign() > 0:
				fundState, fundDetail = readyOK, decimal18(bal)+" FIL"
			default:
				fundState, fundDetail = readyFail, "0 FIL — fund to pay proving gas"
			}
		}
		items = append(items, readinessItem{
			Key: "funded", Label: "PDP wallet funded for gas", State: fundState,
			Detail: fundDetail, Critical: true,
		})
	}

	// 4. Storage stash directory configured.
	stashState, stashDetail := readyFail, "no stash directory configured"
	if s.cfg.StashDir != "" {
		stashState, stashDetail = readyOK, s.cfg.StashDir
	}
	items = append(items, readinessItem{
		Key: "stash", Label: "Piece stash directory set", State: stashState,
		Detail: stashDetail, Critical: true,
	})

	// 5. At least one active dataset (informational — a fresh SP has none).
	dsState, dsDetail := readyWarn, "no active datasets yet"
	if ov.Stats.DatasetsActive > 0 {
		dsState = readyOK
		dsDetail = pluralize(ov.Stats.DatasetsActive, "active dataset")
	}
	items = append(items, readinessItem{
		Key: "datasets", Label: "Serving a dataset", State: dsState, Detail: dsDetail,
	})

	// 6. Proving health over the last 24h.
	proveState, proveDetail := readyOK, "no prove failures in 24h"
	switch {
	case ov.Stats.RecentProveFailed24 > 0:
		proveState = readyFail
		proveDetail = pluralize(ov.Stats.RecentProveFailed24, "prove failure") + " in 24h"
	case ov.Stats.RecentProveSuccess24 == 0 && ov.Stats.DatasetsActive > 0:
		proveState = readyWarn
		proveDetail = "no prove activity in 24h"
	case ov.Stats.RecentProveSuccess24 == 0:
		proveState = readyWarn
		proveDetail = "no prove activity yet"
	default:
		proveDetail = pluralize(ov.Stats.RecentProveSuccess24, "prove task") + " ran, 0 failed"
	}
	items = append(items, readinessItem{
		Key: "proving", Label: "Proving healthy (24h)", State: proveState, Detail: proveDetail,
	})

	rep := readinessReport{Items: items, Total: len(items), AllReady: true}
	for _, it := range items {
		if it.State == readyOK {
			rep.OK++
		}
		if it.Critical && it.State != readyOK {
			rep.AllReady = false
		}
	}
	return rep
}

// pluralize renders "1 dataset" / "3 datasets" for small operator counts.
func pluralize(n int64, noun string) string {
	s := itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}

func itoa(n int64) string { return new(big.Int).SetInt64(n).String() }
