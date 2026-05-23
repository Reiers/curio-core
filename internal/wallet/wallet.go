// Package wallet exposes operator-facing wallet management primitives
// on top of the eth_keys SQLite table.
//
// Today curio-core's signing wallet lives in the eth_keys row with
// role='pdp', auto-generated at boot by internal/ethkeys.Bootstrap.
// Operators running a Hot Storage SP need richer wallet operations:
// inspect what's there, add new keys, import existing keys (so a
// pre-funded wallet can be reused across redeploys), export keys for
// backup, and eventually send FIL/USDFC out of the box.
//
// This package is the SQL surface for those operations. Command-line
// dispatch lives in cmd/curio-core. WebUI dispatch lives in
// internal/setupweb (later).
package wallet

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/curiostorage/harmonyquery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Entry represents one row of the eth_keys table.
type Entry struct {
	Address string `db:"address"`
	Role    string `db:"role"`
}

// List returns every eth_keys row, sorted by role then address.
//
// Read-only and safe to call without the curio-core daemon running
// (the caller is responsible for opening the SQLite DB).
func List(ctx context.Context, db harmonyquery.DBInterface) ([]Entry, error) {
	var rows []Entry
	if err := db.SelectI(ctx, &rows, `
		SELECT address, role FROM eth_keys ORDER BY role, address
	`); err != nil {
		return nil, fmt.Errorf("wallet.List: %w", err)
	}
	return rows, nil
}

// Get returns the eth_keys entry for the requested address, or
// (Entry{}, false) when no row exists.
func Get(ctx context.Context, db harmonyquery.DBInterface, addr common.Address) (Entry, bool, error) {
	var row Entry
	err := db.QueryRowI(ctx, `
		SELECT address, role FROM eth_keys WHERE lower(address) = lower($1)
	`, addr.Hex()).Scan(&row.Address, &row.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "sql: no rows in result set" {
			return Entry{}, false, nil
		}
		return Entry{}, false, fmt.Errorf("wallet.Get: %w", err)
	}
	return row, true, nil
}

// New generates a fresh secp256k1 key, derives its Ethereum address,
// inserts a row with the given role, and returns the new address.
//
// role is operator-defined. Common values: "pdp" (the active signing
// wallet for PDP on-chain writes), "backup", "operator", "test".
// Roles are not interpreted by curio-core except for "pdp" which the
// PDP task pipeline reads.
//
// Returns an error if the role is empty, contains invalid characters,
// or a row with the SAME address already exists.
func New(ctx context.Context, db harmonyquery.DBInterface, role string) (common.Address, error) {
	if err := validateRole(role); err != nil {
		return common.Address{}, err
	}
	priv, err := crypto.GenerateKey()
	if err != nil {
		return common.Address{}, fmt.Errorf("wallet.New: generate key: %w", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	if err := insertKey(ctx, db, addr, crypto.FromECDSA(priv), role); err != nil {
		return common.Address{}, err
	}
	return addr, nil
}

// Import inserts an existing secp256k1 private key under the given
// role. privKeyHex may be 0x-prefixed or bare hex; case-insensitive.
//
// Refuses to overwrite an existing row (caller must Delete first if
// they really mean to swap the key).
func Import(ctx context.Context, db harmonyquery.DBInterface, privKeyHex, role string) (common.Address, error) {
	if err := validateRole(role); err != nil {
		return common.Address{}, err
	}
	keyBytes, err := decodeHex(privKeyHex)
	if err != nil {
		return common.Address{}, fmt.Errorf("wallet.Import: %w", err)
	}
	if len(keyBytes) != 32 {
		return common.Address{}, fmt.Errorf("wallet.Import: secp256k1 private key must be 32 bytes, got %d", len(keyBytes))
	}
	priv, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return common.Address{}, fmt.Errorf("wallet.Import: parse key: %w", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	if err := insertKey(ctx, db, addr, keyBytes, role); err != nil {
		return common.Address{}, err
	}
	return addr, nil
}

// Export returns the raw 32-byte private key for the given address.
//
// Caller is responsible for treating the return value as sensitive
// material: don't log it, don't write to disk, zero the slice after
// use when feasible.
func Export(ctx context.Context, db harmonyquery.DBInterface, addr common.Address) ([]byte, error) {
	var raw []byte
	err := db.QueryRowI(ctx, `
		SELECT private_key FROM eth_keys WHERE lower(address) = lower($1)
	`, addr.Hex()).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "sql: no rows in result set" {
			return nil, fmt.Errorf("wallet.Export: no eth_keys row for %s", addr.Hex())
		}
		return nil, fmt.Errorf("wallet.Export: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("wallet.Export: bad key length %d in DB for %s", len(raw), addr.Hex())
	}
	return raw, nil
}

// Delete removes an eth_keys row by address. Returns whether a row was
// actually removed.
//
// Refuses to delete the last 'pdp' row (the SP would have no signing
// wallet). Use SetRole first if the operator wants to demote it.
func Delete(ctx context.Context, db harmonyquery.DBInterface, addr common.Address) (bool, error) {
	row, ok, err := Get(ctx, db, addr)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	if row.Role == "pdp" {
		// Count pdp rows; refuse if we'd remove the last one.
		var others int
		if err := db.QueryRowI(ctx, `
			SELECT COUNT(*) FROM eth_keys WHERE role = 'pdp' AND lower(address) != lower($1)
		`, addr.Hex()).Scan(&others); err != nil {
			return false, fmt.Errorf("wallet.Delete: count pdp rows: %w", err)
		}
		if others == 0 {
			return false, fmt.Errorf("wallet.Delete: refusing to delete the last role='pdp' wallet; demote with SetRole first")
		}
	}
	n, err := db.ExecI(ctx, `DELETE FROM eth_keys WHERE lower(address) = lower($1)`, addr.Hex())
	if err != nil {
		return false, fmt.Errorf("wallet.Delete: %w", err)
	}
	return n > 0, nil
}

// SetRole changes the role of an existing eth_keys row.
//
// Use case: operator wants to swap which wallet is the active signing
// wallet. SetRole(existing, "backup") then SetRole(other, "pdp"), in
// that order. (Two pdp rows is allowed but the PDP task pipeline picks
// one arbitrarily; better to maintain a single pdp row at a time.)
func SetRole(ctx context.Context, db harmonyquery.DBInterface, addr common.Address, newRole string) error {
	if err := validateRole(newRole); err != nil {
		return err
	}
	if _, ok, err := Get(ctx, db, addr); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("wallet.SetRole: no eth_keys row for %s", addr.Hex())
	}
	if _, err := db.ExecI(ctx, `
		UPDATE eth_keys SET role = $1 WHERE lower(address) = lower($2)
	`, newRole, addr.Hex()); err != nil {
		return fmt.Errorf("wallet.SetRole: %w", err)
	}
	return nil
}

// insertKey is the shared INSERT path used by New + Import.
func insertKey(ctx context.Context, db harmonyquery.DBInterface, addr common.Address, privKey []byte, role string) error {
	// Pre-check: refuse to clobber. eth_keys has address as PRIMARY KEY,
	// so an INSERT collision would error anyway, but a friendlier
	// message here helps the operator.
	if _, ok, err := Get(ctx, db, addr); err != nil {
		return fmt.Errorf("wallet: pre-check existing key: %w", err)
	} else if ok {
		return fmt.Errorf("wallet: address %s already in eth_keys (delete first if you mean to replace)", addr.Hex())
	}
	if _, err := db.ExecI(ctx, `
		INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, $3)
	`, addr.Hex(), privKey, role); err != nil {
		return fmt.Errorf("wallet: insert eth_keys row: %w", err)
	}
	return nil
}

// validateRole enforces the small role-name vocabulary curio-core
// recognises. We don't restrict to a fixed enum (operators may want
// custom roles for their own labelling), but we do reject pathological
// inputs.
func validateRole(role string) error {
	if role == "" {
		return fmt.Errorf("wallet: role is required (e.g. 'pdp', 'backup', 'operator')")
	}
	if len(role) > 32 {
		return fmt.Errorf("wallet: role too long (max 32 chars)")
	}
	for _, c := range role {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return fmt.Errorf("wallet: role %q contains invalid character %q (allowed: a-z A-Z 0-9 _ -)", role, c)
		}
	}
	return nil
}

// decodeHex accepts 0x-prefixed or bare hex.
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd hex length")
	}
	return hex.DecodeString(s)
}
