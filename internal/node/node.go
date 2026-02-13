package node

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Reiers/curio-core/internal/config"
	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
)

func StartSkeleton(log *logging.Logger, st *status.Store, cfg *config.Config) error {
	if err := os.MkdirAll(cfg.NetworkDataDir(), 0o755); err != nil {
		return err
	}
	lock := filepath.Join(cfg.NetworkDataDir(), "node.lock")
	if err := os.WriteFile(lock, []byte(time.Now().Format(time.RFC3339)), 0o644); err != nil {
		return err
	}
	_ = st.Set("syncing", 10, "node skeleton started")
	meta := filepath.Join(cfg.NetworkDataDir(), "node.meta")
	return os.WriteFile(meta, []byte(fmt.Sprintf("network=%s\nstarted=%s\n", cfg.Network, time.Now().Format(time.RFC3339))), 0o644)
}
