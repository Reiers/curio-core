// Package ethkeys bootstraps the eth_keys SQLite row that upstream
// curio/pdp's SenderETH reads to sign FEVM transactions.
//
// Behavior:
//   - First-boot path: generate a random secp256k1 key, derive its
//     Ethereum address, INSERT INTO eth_keys (address, private_key, role)
//     VALUES (?, ?, 'pdp'). Print the address + a faucet hint.
//   - Subsequent boots: detect the existing row, log the address.
//
// The wallet is bound to the curio-core SQLite database, not to the
// embedded Lantern keystore (which holds Filecoin-side BLS/Secp keys
// for ChainNotify or message signing). This mirrors how upstream
// Curio splits the two: PDP signs eth-shaped FEVM txes with raw
// secp256k1 keys in eth_keys; Lotus messages sign with the wallet.
//
// Operators who want to import an existing funded wallet can:
//   - sqlite3 state.sqlite "INSERT INTO eth_keys VALUES ('0x...', X'<32-byte-hex>', 'pdp')"
//   - or use the curio-core CLI config setter (curio-core#8, not yet shipped)
package ethkeys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/curiostorage/harmonyquery"
	"github.com/ethereum/go-ethereum/crypto"
)

// Bootstrap idempotently ensures an eth_keys row with role='pdp' exists.
// Returns the eth address ("0x..." hex, lowercase) of the active key.
//
// On a fresh database this generates a new secp256k1 key. On subsequent
// runs the existing key is detected and returned unchanged.
//
// The generated key is written to the eth_keys table, NOT exported to
// the operator. To back up or move the wallet:
//
//	sqlite3 <data-dir>/state.sqlite \
//	    "SELECT address, hex(private_key) FROM eth_keys WHERE role='pdp'"
func Bootstrap(ctx context.Context, db harmonyquery.DBInterface) (string, error) {
	// Fast path: a row already exists.
	var existingAddr string
	err := db.QueryRowI(ctx, `
		SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1
	`).Scan(&existingAddr)
	switch {
	case err == nil:
		return existingAddr, nil
	case errors.Is(err, sql.ErrNoRows), err.Error() == "sql: no rows in result set":
		// fall through to generation
	default:
		return "", fmt.Errorf("ethkeys.Bootstrap: probe existing key: %w", err)
	}

	// No row yet: generate a fresh secp256k1 key, derive the eth address,
	// and insert. Same shape as upstream curio's web/api/webrpc/pdp.go
	// ImportPDPKey, minus the operator-supplied key path.
	priv, err := crypto.GenerateKey()
	if err != nil {
		return "", fmt.Errorf("ethkeys.Bootstrap: generate secp256k1 key: %w", err)
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	privBytes := crypto.FromECDSA(priv) // 32-byte raw scalar

	if _, err := db.ExecI(ctx, `
		INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, 'pdp')
	`, addr, privBytes); err != nil {
		return "", fmt.Errorf("ethkeys.Bootstrap: insert eth_keys row: %w", err)
	}
	return addr, nil
}

// LookupPDP returns the active PDP eth address, or "" if no row exists.
// Read-only; safe to call from inspection paths.
func LookupPDP(ctx context.Context, db harmonyquery.DBInterface) (string, error) {
	var addr string
	err := db.QueryRowI(ctx, `
		SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1
	`).Scan(&addr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || err.Error() == "sql: no rows in result set" {
			return "", nil
		}
		return "", err
	}
	return addr, nil
}
