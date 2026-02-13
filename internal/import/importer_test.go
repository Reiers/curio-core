package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/store"
)

func TestImportBuildsChainstoreAndBlockstore(t *testing.T) {
	tmp := t.TempDir()
	snap := filepath.Join(tmp, "s.car")
	if err := os.WriteFile(snap, []byte("abcdef0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := status.NewStore(filepath.Join(tmp, "status.json"))
	if err := ImportFile(logging.New(), st, snap, filepath.Join(tmp, "data")); err != nil {
		t.Fatal(err)
	}
	cs := store.NewChainStore(filepath.Join(tmp, "data"))
	h, err := cs.Head()
	if err != nil {
		t.Fatal(err)
	}
	if h.Height < 1 {
		t.Fatalf("expected imported head height >0, got %d", h.Height)
	}
	bs := store.NewBlockstore(filepath.Join(tmp, "data"))
	n, err := bs.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatal("expected blockstore populated")
	}
}
