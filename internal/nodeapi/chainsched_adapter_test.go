package nodeapi

import (
	"testing"

	lotusapi "github.com/filecoin-project/lotus/api"
	lotustypes "github.com/filecoin-project/lotus/chain/types"
)

// hc builds a HeadChange of the given type. Val is a non-nil sentinel
// tipset pointer so the coalescer's type-only logic is exercised without
// constructing real tipsets.
func hc(typ string) *lotusapi.HeadChange {
	return &lotusapi.HeadChange{Type: typ, Val: &lotustypes.TipSet{}}
}

func types_(batch []*lotusapi.HeadChange) []string {
	out := make([]string, len(batch))
	for i, b := range batch {
		out[i] = b.Type
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBatchIsRevertOnly(t *testing.T) {
	cases := []struct {
		name  string
		batch []*lotusapi.HeadChange
		want  bool
	}{
		{"empty => false", nil, false},
		{"single revert => true", []*lotusapi.HeadChange{hc("revert")}, true},
		{"two reverts => true", []*lotusapi.HeadChange{hc("revert"), hc("revert")}, true},
		{"single apply => false", []*lotusapi.HeadChange{hc("apply")}, false},
		{"revert+apply => false", []*lotusapi.HeadChange{hc("revert"), hc("apply")}, false},
		{"current => false", []*lotusapi.HeadChange{hc("current")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := batchIsRevertOnly(tc.batch); got != tc.want {
				t.Errorf("batchIsRevertOnly = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRevertCoalescer_MicroReorg(t *testing.T) {
	var c revertCoalescer

	// apply batch passes straight through.
	if got := c.push([]*lotusapi.HeadChange{hc("apply"), hc("apply")}); !eq(types_(got), []string{"apply", "apply"}) {
		t.Fatalf("apply batch should pass through, got %v", types_(got))
	}

	// revert-only batch is held (returns nil).
	if got := c.push([]*lotusapi.HeadChange{hc("revert")}); got != nil {
		t.Fatalf("revert-only batch should be held, got %v", types_(got))
	}

	// next apply batch gets the held revert prepended.
	got := c.push([]*lotusapi.HeadChange{hc("apply"), hc("apply")})
	if !eq(types_(got), []string{"revert", "apply", "apply"}) {
		t.Fatalf("merged batch = %v, want [revert apply apply]", types_(got))
	}

	// nothing left to flush.
	if f := c.flush(); f != nil {
		t.Fatalf("flush should be empty, got %v", types_(f))
	}
}

func TestRevertCoalescer_BackToBackReverts(t *testing.T) {
	var c revertCoalescer
	if got := c.push([]*lotusapi.HeadChange{hc("revert")}); got != nil {
		t.Fatalf("first revert-only held, got %v", types_(got))
	}
	if got := c.push([]*lotusapi.HeadChange{hc("revert")}); got != nil {
		t.Fatalf("second revert-only held, got %v", types_(got))
	}
	got := c.push([]*lotusapi.HeadChange{hc("apply")})
	if !eq(types_(got), []string{"revert", "revert", "apply"}) {
		t.Fatalf("deep-reorg merge = %v, want [revert revert apply]", types_(got))
	}
}

func TestRevertCoalescer_FlushOnClose(t *testing.T) {
	var c revertCoalescer
	// A trailing revert-only batch at stream close must NOT be dropped.
	if got := c.push([]*lotusapi.HeadChange{hc("revert")}); got != nil {
		t.Fatalf("revert held, got %v", types_(got))
	}
	f := c.flush()
	if !eq(types_(f), []string{"revert"}) {
		t.Fatalf("flush = %v, want [revert]", types_(f))
	}
	// second flush is empty.
	if f2 := c.flush(); f2 != nil {
		t.Fatalf("second flush should be empty, got %v", types_(f2))
	}
}

func TestRevertCoalescer_RevertApplyBatchPassesThrough(t *testing.T) {
	var c revertCoalescer
	// A batch that already carries both revert and apply is not held.
	got := c.push([]*lotusapi.HeadChange{hc("revert"), hc("apply")})
	if !eq(types_(got), []string{"revert", "apply"}) {
		t.Fatalf("revert+apply batch should pass through, got %v", types_(got))
	}
	if f := c.flush(); f != nil {
		t.Fatalf("nothing should be pending, got %v", types_(f))
	}
}
