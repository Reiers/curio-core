package node

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/store"
	syncpipe "github.com/Reiers/curio-core/internal/sync"
)

func StartSkeleton(log *logging.Logger, st *status.Store, cfg *config.Config) error {
	if err := os.MkdirAll(cfg.NetworkDataDir(), 0o755); err != nil {
		return err
	}
	lock := filepath.Join(cfg.NetworkDataDir(), "node.lock")
	if err := os.WriteFile(lock, []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		return err
	}
	_ = st.Set("syncing", 10, "node started")
	meta := filepath.Join(cfg.NetworkDataDir(), "node.meta")
	if err := os.WriteFile(meta, []byte(fmt.Sprintf("network=%s\nstarted=%s\n", cfg.Network, time.Now().Format(time.RFC3339))), 0o644); err != nil {
		return err
	}

	cs := store.NewChainStore(cfg.NetworkDataDir())
	if _, err := cs.Head(); err != nil {
		_ = cs.SetHead(store.Head{Height: 0, TipSetKey: "genesis", StateRoot: "genesis"})
	}
	pipe := syncpipe.NewPipeline(log, st, cs)
	if err := pipe.RunIncremental(3); err != nil {
		return err
	}
	_ = st.Set("syncing", 100, "node synced incremental head updates")
	return nil
}
