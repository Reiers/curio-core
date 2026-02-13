package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Head struct {
	Height    int64     `json:"height"`
	TipSetKey string    `json:"tipsetKey"`
	StateRoot string    `json:"stateRoot,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ChainStore struct {
	baseDir  string
	headFile string
}

func NewChainStore(baseDir string) *ChainStore {
	return &ChainStore{baseDir: filepath.Join(baseDir, "chainstore"), headFile: filepath.Join(baseDir, "chainstore", "head.json")}
}

func (c *ChainStore) Init() error { return os.MkdirAll(c.baseDir, 0o755) }

func (c *ChainStore) SetHead(h Head) error {
	if err := c.Init(); err != nil {
		return err
	}
	h.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.headFile, b, 0o644)
}

func (c *ChainStore) Head() (*Head, error) {
	if err := c.Init(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(c.headFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("head not found")
		}
		return nil, err
	}
	var h Head
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	return &h, nil
}
