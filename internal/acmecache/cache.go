// Package acmecache implements golang.org/x/crypto/acme/autocert.Cache
// backed by the curio-core SQLite state DB (autocert_cache table).
//
// autocert persists the ACME account key, issued certificates, and
// renewal state via this Cache. Storing it in the same SQLite DB the
// rest of curio-core uses means a single-binary SP needs no external
// cert store and survives restarts without a fresh ACME round-trip —
// the two-port + baked-in-TLS goal of curio-core#69.
//
// Schema (internal/harmonysqlite/schema-curio-core/0014_infra_misc.sql):
//
//	CREATE TABLE autocert_cache ( k TEXT PRIMARY KEY, v BLOB NOT NULL );
package acmecache

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/acme/autocert"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// Cache is an autocert.Cache over the autocert_cache table.
type Cache struct {
	db *harmonysqlite.DB
}

// New returns a Cache backed by the given state DB. The autocert_cache
// table is created by migration 0014; New does not create it.
func New(db *harmonysqlite.DB) *Cache {
	return &Cache{db: db}
}

// Get returns the value for key, or autocert.ErrCacheMiss if absent.
func (c *Cache) Get(ctx context.Context, key string) ([]byte, error) {
	var v []byte
	err := c.db.QueryRow(ctx, `SELECT v FROM autocert_cache WHERE k = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, autocert.ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("acmecache: get %q: %w", key, err)
	}
	return v, nil
}

// Put stores data under key, overwriting any existing value.
func (c *Cache) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.db.Exec(ctx, `
		INSERT INTO autocert_cache (k, v) VALUES (?, ?)
		ON CONFLICT(k) DO UPDATE SET v = excluded.v`, key, data)
	if err != nil {
		return fmt.Errorf("acmecache: put %q: %w", key, err)
	}
	return nil
}

// Delete removes key. Absent keys are not an error.
func (c *Cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.Exec(ctx, `DELETE FROM autocert_cache WHERE k = ?`, key)
	if err != nil {
		return fmt.Errorf("acmecache: delete %q: %w", key, err)
	}
	return nil
}

// Compile-time guard: Cache satisfies autocert.Cache.
var _ autocert.Cache = (*Cache)(nil)
