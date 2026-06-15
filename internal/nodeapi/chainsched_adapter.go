// chainsched_adapter.go — bridge embedded Lantern's HeadChanges to
// the lotus-typed surface upstream chainsched expects.
//
// Lantern's embedded JSON-RPC server is HTTP POST (no WebSocket, no
// streaming channels), so calling Filecoin.ChainNotify via the
// standard lotusclient errors with "method not supported in this
// mode (no out channel support)". Lantern V1.5 (this PR's pair commit
// in pkg/daemon) wires a head-change distributor + accessor
// Daemon.HeadChanges() that bypasses the RPC layer entirely.
//
// EmbeddedChainSchedNodeAPI wraps a lotusapi.FullNode (for ChainHead
// — a simple POST-shaped RPC that works fine over HTTP) plus a
// *lanterndaemon.Daemon (for ChainNotify via the in-process
// distributor). The result satisfies chainsched.NodeAPI.
//
// Type bridging: Lantern's chain/types.TipSet and Lotus's
// chain/types.TipSet are independent Go types with identical CBOR
// schemas (Lantern was vendored from lotus at a specific commit; see
// chain/types/tipset.go's preamble). We re-encode through CBOR per
// tipset; that's cheap (one tipset every block, ~30s on calibration)
// and avoids forking either type or sharing a header definition.

package nodeapi

import (
	"bytes"
	"context"
	"fmt"

	lotusapi "github.com/filecoin-project/lotus/api"
	lotustypes "github.com/filecoin-project/lotus/chain/types"

	lanterntypes "github.com/Reiers/lantern/chain/types"
	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"
)

// EmbeddedChainSchedNodeAPI satisfies the chainsched.NodeAPI interface
// used by curio's upstream chain-scheduler. It delegates ChainHead to
// the lotus-typed FullNode (a regular HTTP-POST RPC call) and bridges
// ChainNotify to the in-process Lantern head-change distributor so
// streaming subscriptions don't require WebSocket transport.
//
// Construct via NewEmbeddedChainSchedNodeAPI. Pass the same nodeapi
// Client whose FullNode is used elsewhere in curio-core (PDPService,
// SP register/info commands).
type EmbeddedChainSchedNodeAPI struct {
	full   lotusapi.FullNode
	daemon *lanterndaemon.Daemon
}

// NewEmbeddedChainSchedNodeAPI wraps an existing FullNode + embedded
// Daemon. Returns an error if daemon is nil or hasn't been started
// (the head-change distributor isn't ready until Start completes).
func NewEmbeddedChainSchedNodeAPI(full lotusapi.FullNode, daemon *lanterndaemon.Daemon) (*EmbeddedChainSchedNodeAPI, error) {
	if full == nil {
		return nil, fmt.Errorf("nodeapi: FullNode is nil")
	}
	if daemon == nil {
		return nil, fmt.Errorf("nodeapi: daemon is nil (HeadChanges requires the in-process daemon)")
	}
	if !daemon.Started() {
		return nil, fmt.Errorf("nodeapi: daemon not started")
	}
	return &EmbeddedChainSchedNodeAPI{full: full, daemon: daemon}, nil
}

// ChainHead delegates to the wrapped FullNode. Plain HTTP-POST RPC;
// no streaming surface required.
func (e *EmbeddedChainSchedNodeAPI) ChainHead(ctx context.Context) (*lotustypes.TipSet, error) {
	return e.full.ChainHead(ctx)
}

// ChainNotify subscribes to the in-process head-change distributor
// via Daemon.HeadChanges and bridges each event to the lotus-typed
// shape the upstream chainsched expects.
//
// The returned channel is buffered (matches the distributor's
// per-subscriber buffer size). Cancelling ctx unsubscribes upstream
// AND closes the returned channel (the converter goroutine exits
// when its source channel closes).
//
// Conversion errors (an unlikely CBOR re-encode failure) are logged
// at the bridge layer and the offending event is dropped; the
// subscription stays alive.
func (e *EmbeddedChainSchedNodeAPI) ChainNotify(ctx context.Context) (<-chan []*lotusapi.HeadChange, error) {
	src := e.daemon.HeadChanges(ctx)
	if src == nil {
		return nil, fmt.Errorf("nodeapi: daemon HeadChanges unavailable (NoHeaderStore?)")
	}

	out := make(chan []*lotusapi.HeadChange, 16)
	go func() {
		defer close(out)
		// pendingReverts holds a revert-only batch until the next batch
		// arrives, then prepends it (curio-core#78). On a 1-epoch
		// calibration micro-reorg the header store fires OnHeadChange
		// twice: a revert-only batch (head regressed to the divergence
		// tipset) immediately followed by the re-apply batch. Upstream
		// chainsched's update(lowest,highest) logs ERROR "no new tipset"
		// for the revert-only batch because its apply side is nil.
		//
		// We merge the two: hold a revert-only batch and emit it joined
		// to the front of the next batch's events. This is a pure
		// SEQUENCING merge, not a timed hold — the distributor delivers
		// batches in order on a single channel, and a revert-only batch is
		// always immediately succeeded by its re-apply batch. No timer, no
		// race. If the source closes with a revert still pending (shutdown
		// mid-reorg), we flush it so no revert is ever dropped — chainsched
		// then logs one benign "no new tipset" at teardown, which is fine.
		var coalescer revertCoalescer
		emit := func(events []*lotusapi.HeadChange) bool {
			select {
			case out <- events:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for batch := range src {
			converted := make([]*lotusapi.HeadChange, 0, len(batch))
			for _, hc := range batch {
				lotusTS, err := convertLanternTipSet(hc.Val)
				if err != nil {
					// Silently drop the malformed event; chainsched
					// will catch up on the next tipset. This codepath
					// has no logger handle and we don't want to fail
					// the whole subscription on one bad re-encode.
					continue
				}
				// Startup transient: the distributor emits the first
				// {Type:"current"} event before the embedded header
				// store has seeded a head, so Val is nil. Upstream
				// chainsched takes its first-notification path and
				// calls update(nil, nil) -> logs ERROR "no new tipset".
				// Substitute the real head from ChainHead so the first
				// "current" carries a valid tipset. If ChainHead also
				// can't produce one yet, drop the event; chainsched
				// resubscribes and gets a seeded "current" next round.
				if lotusTS == nil && hc.Type == "current" {
					head, herr := e.full.ChainHead(ctx)
					if herr != nil || head == nil {
						continue
					}
					lotusTS = head
				}
				converted = append(converted, &lotusapi.HeadChange{
					Type: hc.Type,
					Val:  lotusTS,
				})
			}
			if len(converted) == 0 {
				continue
			}
			if merged := coalescer.push(converted); merged != nil {
				if !emit(merged) {
					return
				}
			}
		}
		// Source closed. Flush any still-pending reverts so none are lost.
		if flush := coalescer.flush(); flush != nil {
			_ = emit(flush)
		}
	}()
	return out, nil
}

// revertCoalescer merges a revert-only HeadChange batch into the next
// batch so downstream chainsched never sees a revert with no apply
// (curio-core#78). It is a pure sequencing merge over the ordered
// distributor stream: push returns nil to hold a revert-only batch, or
// the batch (with any held reverts prepended) when an apply-bearing batch
// arrives. flush drains a trailing held revert at stream close.
type revertCoalescer struct {
	pending []*lotusapi.HeadChange
}

func batchIsRevertOnly(batch []*lotusapi.HeadChange) bool {
	if len(batch) == 0 {
		return false
	}
	for _, hc := range batch {
		if hc.Type != "revert" {
			return false
		}
	}
	return true
}

// push consumes one converted batch. Returns the batch to emit (possibly
// with held reverts prepended), or nil if the batch was a revert-only
// batch held for merging.
func (c *revertCoalescer) push(batch []*lotusapi.HeadChange) []*lotusapi.HeadChange {
	if batchIsRevertOnly(batch) {
		// Accumulate in case multiple revert-only batches arrive
		// back-to-back (a deeper reorg).
		c.pending = append(c.pending, batch...)
		return nil
	}
	if len(c.pending) > 0 {
		merged := append(c.pending, batch...)
		c.pending = nil
		return merged
	}
	return batch
}

// flush returns any held reverts at stream close so none are dropped.
func (c *revertCoalescer) flush() []*lotusapi.HeadChange {
	if len(c.pending) == 0 {
		return nil
	}
	out := c.pending
	c.pending = nil
	return out
}

// convertLanternTipSet re-encodes a Lantern TipSet through its block
// headers as the canonical CBOR format and rebuilds a lotus TipSet
// from the same bytes. Both types vendor identical CBOR schemas, so
// the round-trip is lossless.
//
// Returns (nil, nil) when ts is nil (the distributor emits nil-Val
// "current" events when the store hasn't seen a head yet).
func convertLanternTipSet(ts *lanterntypes.TipSet) (*lotustypes.TipSet, error) {
	if ts == nil {
		return nil, nil
	}
	srcBlocks := ts.Blocks()
	if len(srcBlocks) == 0 {
		// chainsched.update treats nil tipsets as "no apply" and skips,
		// so returning nil here is safe.
		return nil, nil
	}
	dstBlocks := make([]*lotustypes.BlockHeader, len(srcBlocks))
	var buf bytes.Buffer
	for i, src := range srcBlocks {
		buf.Reset()
		if err := src.MarshalCBOR(&buf); err != nil {
			return nil, fmt.Errorf("marshal lantern BlockHeader: %w", err)
		}
		dst := new(lotustypes.BlockHeader)
		if err := dst.UnmarshalCBOR(bytes.NewReader(buf.Bytes())); err != nil {
			return nil, fmt.Errorf("unmarshal lotus BlockHeader: %w", err)
		}
		dstBlocks[i] = dst
	}
	return lotustypes.NewTipSet(dstBlocks)
}
