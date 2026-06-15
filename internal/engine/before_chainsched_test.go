package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	lotusapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// fakeNodeAPI is the minimal chainsched.NodeAPI: ChainNotify returns a
// channel that never fires, so Run() blocks in its subscription loop
// without needing a real chain. ChainHead is unused by Run's entry path.
type fakeNodeAPI struct{}

func (fakeNodeAPI) ChainHead(context.Context) (*types.TipSet, error) { return nil, nil }
func (fakeNodeAPI) ChainNotify(ctx context.Context) (<-chan []*lotusapi.HeadChange, error) {
	ch := make(chan []*lotusapi.HeadChange)
	return ch, nil
}

// TestOnBeforeChainSched_RegistersBeforeStart is the regression guard for
// curio-core#81: the eth message watcher must register its chainSched
// watcher BEFORE chainSched.Run() flips started=true (after which
// AddWatcher errors with "cannot add watcher handler after start").
//
// We register a callback via OnBeforeChainSched that calls
// sched.AddWatcher — exactly what NewMessageWatcherEth does internally.
// If the engine fired the callback too late (the #81 bug), AddWatcher
// would error and Start would fail.
func TestOnBeforeChainSched_RegistersBeforeStart(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })

	sched := chainsched.New(fakeNodeAPI{})
	e.SetChainSched(sched)

	var (
		called      atomic.Bool
		gotTE       atomic.Bool
		addWatchErr atomic.Value // error
	)
	e.OnBeforeChainSched(func(te *harmonytask.TaskEngine, s *chainsched.CurioChainSched) error {
		called.Store(true)
		gotTE.Store(te != nil)
		// This is the call that #81 broke: AddWatcher after the scheduler
		// has started returns an error. If our ordering is right, the
		// scheduler is NOT yet started here and this succeeds.
		if err := s.AddWatcher(func(context.Context, *types.TipSet, *types.TipSet) error {
			return nil
		}); err != nil {
			addWatchErr.Store(err)
		}
		return nil
	})

	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !called.Load() {
		t.Fatal("OnBeforeChainSched callback never fired")
	}
	if !gotTE.Load() {
		t.Error("callback received a nil TaskEngine; eth watcher needs a live engine ref")
	}
	if v := addWatchErr.Load(); v != nil {
		t.Fatalf("AddWatcher inside OnBeforeChainSched failed (the #81 regression): %v", v.(error))
	}

	// Sanity: the scheduler did start (Run goroutine is up) shortly after.
	// Give it a beat; we can't observe started directly, but AddWatcher
	// now SHOULD fail, proving Run flipped started=true after our callback.
	time.Sleep(50 * time.Millisecond)
	if err := sched.AddWatcher(func(context.Context, *types.TipSet, *types.TipSet) error { return nil }); err == nil {
		t.Error("expected AddWatcher to fail after Start (scheduler running); ordering may be wrong")
	}
}

// TestOnBeforeChainSched_PanicsAfterStart guards the misuse contract:
// registering a hook after Start must panic, like SetChainSched.
func TestOnBeforeChainSched_PanicsAfterStart(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Config{DBPath: ":memory:"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Stop() })
	if err := e.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Error("OnBeforeChainSched after Start did not panic")
		}
	}()
	e.OnBeforeChainSched(func(*harmonytask.TaskEngine, *chainsched.CurioChainSched) error { return nil })
}
