package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
	"github.com/Reiers/curio-core/internal/store"
)

func ImportFile(log *logging.Logger, st *status.Store, snapshotFile, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	fi, err := os.Stat(snapshotFile)
	if err != nil {
		return err
	}
	total := fi.Size()
	f, err := os.Open(snapshotFile)
	if err != nil {
		return err
	}
	defer f.Close()

	out, err := os.Create(filepath.Join(dataDir, "imported.snapshot.meta"))
	if err != nil {
		return err
	}
	defer out.Close()

	bs := store.NewBlockstore(dataDir)
	cs := store.NewChainStore(dataDir)
	if err := bs.Init(); err != nil {
		return err
	}
	if err := cs.Init(); err != nil {
		return err
	}

	buf := make([]byte, 1024*1024)
	var read int64
	var lastCID string
	var blocks int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			cid, putErr := bs.Put(chunk)
			if putErr != nil {
				return putErr
			}
			lastCID = cid
			blocks++
			read += int64(n)
			pct := int((float64(read) / float64(total)) * 100)
			if pct > 100 {
				pct = 100
			}
			_ = st.Set("importing", pct, fmt.Sprintf("importing blocks/state (%d/%d bytes)", read, total))
			_, _ = out.WriteString(fmt.Sprintf("%d/%d blocks=%d head=%s\n", read, total, blocks, lastCID))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if err := cs.SetHead(store.Head{Height: blocks, TipSetKey: lastCID, StateRoot: lastCID}); err != nil {
		return err
	}
	_ = st.Set("importing", 100, fmt.Sprintf("snapshot import complete (height=%d)", blocks))
	log.Infof("import complete from %s; head=%s height=%d", snapshotFile, lastCID, blocks)
	return nil
}
