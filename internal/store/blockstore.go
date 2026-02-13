package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Blockstore struct{ dir string }

func NewBlockstore(baseDir string) *Blockstore {
	return &Blockstore{dir: filepath.Join(baseDir, "blockstore")}
}

func (b *Blockstore) Init() error { return os.MkdirAll(b.dir, 0o755) }

func (b *Blockstore) Put(data []byte) (string, error) {
	if err := b.Init(); err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	cid := hex.EncodeToString(h[:])
	p := filepath.Join(b.dir, cid+".blk")
	if _, err := os.Stat(p); err == nil {
		return cid, nil
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return cid, nil
}

func (b *Blockstore) Has(cid string) bool {
	_, err := os.Stat(filepath.Join(b.dir, cid+".blk"))
	return err == nil
}

func (b *Blockstore) Count() (int, error) {
	if err := b.Init(); err != nil {
		return 0, err
	}
	ents, err := os.ReadDir(b.dir)
	if err != nil {
		return 0, err
	}
	return len(ents), nil
}
