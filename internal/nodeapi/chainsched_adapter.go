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

	lanterndaemon "github.com/Reiers/lantern/pkg/daemon"
	lanterntypes "github.com/Reiers/lantern/chain/types"
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
			select {
			case out <- converted:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
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
