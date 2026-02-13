package importer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Reiers/curio-core/internal/logging"
	"github.com/Reiers/curio-core/internal/status"
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

	buf := make([]byte, 4*1024*1024)
	var read int64
	for {
		n, err := f.Read(buf)
		if n > 0 {
			read += int64(n)
			pct := int((float64(read) / float64(total)) * 100)
			if pct > 100 {
				pct = 100
			}
			_ = st.Set("importing", pct, fmt.Sprintf("importing blocks/state (%d/%d bytes)", read, total))
			_, _ = out.WriteString(fmt.Sprintf("%d/%d\n", read, total))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	_ = st.Set("importing", 100, "snapshot import complete")
	log.Infof("import complete from %s", snapshotFile)
	return nil
}
