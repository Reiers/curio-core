package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Config struct {
	HomeDir             string `json:"homeDir"`
	DataDir             string `json:"dataDir"`
	SnapshotDir         string `json:"snapshotDir"`
	StatusFile          string `json:"statusFile"`
	ConfigFile          string `json:"-"`
	Network             string `json:"network"`
	Mode                string `json:"mode"`
	SnapshotKeep        bool   `json:"snapshotKeep"`
	SnapshotURLOverride string `json:"snapshotURLOverride,omitempty"`
}

func Default(home string) *Config {
	return &Config{
		HomeDir:     home,
		DataDir:     filepath.Join(home, "data"),
		SnapshotDir: filepath.Join(home, "snapshots"),
		StatusFile:  filepath.Join(home, "status.json"),
		ConfigFile:  filepath.Join(home, "config.json"),
		Network:     "mainnet",
		Mode:        "fast",
	}
}

func LoadOrDefault(home string) (*Config, error) {
	cfg := Default(home)
	b, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, err
	}
	cfg.ConfigFile = filepath.Join(cfg.HomeDir, "config.json")
	return cfg, nil
}

func Save(cfg *Config, force bool) error {
	if !force {
		if _, err := os.Stat(cfg.ConfigFile); err == nil {
			return errors.New("config already exists; use --force")
		}
	}
	if err := os.MkdirAll(cfg.HomeDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfg.ConfigFile, b, 0o644)
}

func InitDirs(cfg *Config) error {
	for _, d := range []string{cfg.HomeDir, cfg.DataDir, cfg.SnapshotDir, filepath.Dir(cfg.StatusFile), cfg.NetworkDataDir(), filepath.Join(cfg.SnapshotDir, cfg.Network)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) ResolveSnapshotURL() string {
	if c.SnapshotURLOverride != "" {
		return c.SnapshotURLOverride
	}
	if c.Network == "calibnet" {
		return "https://forest-archive.chainsafe.dev/latest/calibnet/"
	}
	return "https://forest-archive.chainsafe.dev/latest/mainnet/"
}

func (c *Config) NetworkDataDir() string {
	return filepath.Join(c.DataDir, c.Network)
}
