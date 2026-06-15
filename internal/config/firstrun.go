// Package config holds the curio-core first-run detection + the
// minimal config bundle the daemon needs before it can do useful work.
//
// # Storage shape
//
// Config lives in the SQLite `harmony_config` table (one row per
// "layer"; we use the "default" layer for the single curio-core
// instance). The row body is a small TOML document with the fields
// any future PDP scheduling work will reference:
//
//	[Pdp]
//	MarketAddress = "0x..."
//	WalletAddress = "0x..."
//	MinerID       = "f0..."
//
// On a fresh DB the row is absent. `Status` returns NeedsSetup=true
// with `Missing` enumerating every required field. `UpsertDefaultLayer`
// writes (or replaces) the row.
//
// This is intentionally narrower than upstream Curio's config surface:
// curio-core only needs the three identifiers to bootstrap, and the
// `/setup` WebUI flow (see `internal/setupweb`) collects them.
package config

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// DefaultLayerName is the harmony_config.title value curio-core uses
// for its single, canonical config layer.
const DefaultLayerName = "default"

// ConfigBundle is the curio-core minimal config record. It maps to
// the TOML stored in harmony_config.config under title='default'.
//
// All three fields are required for NeedsSetup to flip to false.
type ConfigBundle struct {
	Pdp PdpSection `toml:"Pdp"`
}

// PdpSection holds the PDP-specific identifiers.
type PdpSection struct {
	MarketAddress string `toml:"MarketAddress"`
	WalletAddress string `toml:"WalletAddress"`
	MinerID       string `toml:"MinerID"`
}

// FirstRunStatus reports whether the daemon needs a /setup pass.
type FirstRunStatus struct {
	// NeedsSetup is true if any required field is empty or the row
	// is missing entirely.
	NeedsSetup bool

	// Missing enumerates every empty required field in the
	// fixed order: market_address, wallet_address, miner_id.
	// Empty when NeedsSetup is false.
	Missing []string
}

// Required field tokens, in deterministic order. Exported because the
// /setup form + the curio-core CLI surface both render these
// verbatim.
const (
	FieldMarketAddress = "market_address"
	FieldWalletAddress = "wallet_address"
	FieldMinerID       = "miner_id"
)

// requiredFields is the canonical ordered list. Used by Status and
// validateBundle to stay in sync.
var requiredFields = []string{FieldMarketAddress, FieldWalletAddress, FieldMinerID}

// Status reads the default layer and reports whether curio-core
// can start serving traffic.
//
// On a fresh DB (row absent) or any field empty, NeedsSetup=true and
// every empty field is listed in Missing.
//
// A genuine DB error (table missing, IO failure) is returned as-is.
// Callers that want a cheap "is this DB fresh?" probe should check
// for sql.ErrNoRows explicitly.
func Status(ctx context.Context, db *harmonysqlite.DB) (FirstRunStatus, error) {
	var blob string
	row := db.QueryRow(ctx,
		`SELECT config FROM harmony_config WHERE title = ?`, DefaultLayerName)
	err := row.Scan(&blob)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return FirstRunStatus{NeedsSetup: true, Missing: append([]string(nil), requiredFields...)}, nil
	case err != nil:
		return FirstRunStatus{}, fmt.Errorf("config: read default layer: %w", err)
	}

	bundle, err := decodeBundle(blob)
	if err != nil {
		return FirstRunStatus{}, fmt.Errorf("config: decode default layer: %w", err)
	}

	missing := missingFields(bundle)
	if len(missing) == 0 {
		return FirstRunStatus{NeedsSetup: false}, nil
	}
	return FirstRunStatus{NeedsSetup: true, Missing: missing}, nil
}

// UpsertDefaultLayer writes (or replaces) the default-layer row with
// the given bundle. Every required field must be non-empty.
//
// On success the row is committed and a subsequent Status() returns
// NeedsSetup=false.
func UpsertDefaultLayer(ctx context.Context, db *harmonysqlite.DB, cfg ConfigBundle) error {
	if err := validateBundle(cfg); err != nil {
		return err
	}
	body, err := encodeBundle(cfg)
	if err != nil {
		return fmt.Errorf("config: encode bundle: %w", err)
	}

	// SQLite UPSERT (title is PRIMARY KEY in 0006_common_layers.sql).
	_, err = db.Exec(ctx, `
		INSERT INTO harmony_config (title, config) VALUES (?, ?)
		ON CONFLICT(title) DO UPDATE SET config = excluded.config`,
		DefaultLayerName, body)
	if err != nil {
		return fmt.Errorf("config: upsert default layer: %w", err)
	}
	return nil
}

// validateBundle returns an error naming the first empty required
// field, or nil if every required field is populated.
func validateBundle(cfg ConfigBundle) error {
	missing := missingFields(cfg)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("config: required fields empty: %s", strings.Join(missing, ", "))
}

// missingFields returns the names of every empty required field, in
// the canonical order.
func missingFields(cfg ConfigBundle) []string {
	var out []string
	if strings.TrimSpace(cfg.Pdp.MarketAddress) == "" {
		out = append(out, FieldMarketAddress)
	}
	if strings.TrimSpace(cfg.Pdp.WalletAddress) == "" {
		out = append(out, FieldWalletAddress)
	}
	if strings.TrimSpace(cfg.Pdp.MinerID) == "" {
		out = append(out, FieldMinerID)
	}
	return out
}

func decodeBundle(blob string) (ConfigBundle, error) {
	var cfg ConfigBundle
	if strings.TrimSpace(blob) == "" {
		return cfg, nil
	}
	if _, err := toml.Decode(blob, &cfg); err != nil {
		return ConfigBundle{}, err
	}
	return cfg, nil
}

func encodeBundle(cfg ConfigBundle) (string, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// --- Partial-set helpers for the curio-core config CLI ---------------
//
// The WebUI flow uses UpsertDefaultLayer (full bundle, all required
// fields). The CLI flow exposes per-field updates so headless installs
// can populate one field at a time. Both write to the same SQLite row;
// they're alternative paths to the same end state.

// ReadDefaultLayer returns the current bundle, or a zero bundle when
// no row exists yet. Unlike Status, the second return value is the
// raw error for callers that need to distinguish "row absent" (treated
// as zero bundle here) from genuine DB failures.
func ReadDefaultLayer(ctx context.Context, db *harmonysqlite.DB) (ConfigBundle, error) {
	var blob string
	row := db.QueryRow(ctx, `SELECT config FROM harmony_config WHERE title = ?`, DefaultLayerName)
	err := row.Scan(&blob)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ConfigBundle{}, nil
	case err != nil:
		return ConfigBundle{}, fmt.Errorf("config: read default layer: %w", err)
	}
	return decodeBundle(blob)
}

// WriteDefaultLayer is the partial-allowed variant of UpsertDefaultLayer.
// Required fields may be empty; callers are responsible for checking
// completeness via Status() before launching curio-core run.
//
// CLI usage: read -> mutate one field -> write. Run-time validation
// remains in Status / the run command's startup check.
func WriteDefaultLayer(ctx context.Context, db *harmonysqlite.DB, cfg ConfigBundle) error {
	body, err := encodeBundle(cfg)
	if err != nil {
		return fmt.Errorf("config: encode bundle: %w", err)
	}
	_, err = db.Exec(ctx, `
		INSERT INTO harmony_config (title, config) VALUES (?, ?)
		ON CONFLICT(title) DO UPDATE SET config = excluded.config`,
		DefaultLayerName, body)
	if err != nil {
		return fmt.Errorf("config: write default layer: %w", err)
	}
	return nil
}

// SetField is the per-field convenience. Reads the current bundle,
// mutates one field, writes back. Unknown field names return an error.
//
// Accepted names (case-insensitive):
//
//	pdp.miner-id / miner_id / minerid    -> Pdp.MinerID
//	pdp.wallet   / wallet_address        -> Pdp.WalletAddress
//	pdp.market   / market_address        -> Pdp.MarketAddress
func SetField(ctx context.Context, db *harmonysqlite.DB, field, value string) error {
	cur, err := ReadDefaultLayer(ctx, db)
	if err != nil {
		return err
	}
	field = strings.ToLower(strings.TrimSpace(field))
	value = strings.TrimSpace(value)
	switch field {
	case "pdp.miner-id", "miner_id", "minerid", "miner-id":
		cur.Pdp.MinerID = value
	case "pdp.wallet", "wallet_address", "wallet":
		cur.Pdp.WalletAddress = value
	case "pdp.market", "market_address", "market":
		cur.Pdp.MarketAddress = value
	default:
		return fmt.Errorf("unknown config field %q (known: pdp.miner-id, pdp.wallet, pdp.market)", field)
	}
	return WriteDefaultLayer(ctx, db, cur)
}
