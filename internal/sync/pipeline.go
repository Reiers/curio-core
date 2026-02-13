package sync

import (
	"fmt"
	"time"

	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/store"
)

type Pipeline struct {
	log *logging.Logger
	st  *status.Store
	cs  *store.ChainStore
}

func NewPipeline(log *logging.Logger, st *status.Store, cs *store.ChainStore) *Pipeline {
	return &Pipeline{log: log, st: st, cs: cs}
}

// RunIncremental performs a minimal vertical-slice sync loop.
func (p *Pipeline) RunIncremental(iterations int) error {
	h, err := p.cs.Head()
	if err != nil {
		return err
	}
	for i := 0; i < iterations; i++ {
		next := h.Height + 1
		_ = p.st.Set("syncing", 90, fmt.Sprintf("incremental sync to height %d", next))
		h.Height = next
		h.TipSetKey = fmt.Sprintf("ts-%d", next)
		if err := p.cs.SetHead(*h); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.log.Infof("incremental sync advanced to %d", h.Height)
	return nil
}
