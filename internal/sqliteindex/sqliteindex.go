// Package sqliteindex is the SQLite-backed implementation of curio's
// `market/indexstore.Backend` interface (introduced in the Reiers/curio
// fork by the same workstream as this package).
//
// curio-core is a hot-storage SP deployment: pure-Go, single-binary,
// SQLite for local state. The upstream `market/indexstore` package is
// Cassandra-backed via gocql, designed for the multi-machine production
// Curio cluster. For curio-core we need a Cassandra-free implementation
// that satisfies the same Backend contract; this package is it.
//
// Scope: only the methods the curio-core active code path uses. Other
// methods on the upstream IndexStore type (payload-to-piece indexing,
// IPNI, mk20 aggregates) either:
//   - aren't called in pdpv0-only deployments (the curio-core scope),
//     so they're implemented as best-effort stubs returning empty
//     results / not-supported errors when called.
//   - or are called only from cold paths (cleanup, IPNI publisher,
//     mk20) which curio-core doesn't activate.
//
// Active surface used by tasks/pdpv0 + lib/cachedreader (as of
// 2026-05-24, against Reiers/curio db-seam-refactor branch):
//
//	AddPDPLayer       INSERT pdp_cache_layer
//	GetPDPLayer       SELECT all leaves for (piece, layer)
//	GetPDPLayerIndex  Detect cached layer existence for a piece
//	GetPDPNode        SELECT single leaf
//	FindPieceInAggregate  cachedreader fallthrough (mk20-only; we
//	                      return empty, callers continue to piecePark)
//
// All operations go through the shared *harmonysqlite.DB the rest of
// curio-core uses; no separate connection or schema lifecycle is needed.

package sqliteindex

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/curio/market/indexstore"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// NodeDigest + Record types are aliased from upstream's market/indexstore.
// The interface contract is defined upstream against those types; sharing
// the aliases here means *Store satisfies indexstore.Backend without any
// translation at the call site.
type NodeDigest = indexstore.NodeDigest
type Record = indexstore.Record

// Store is the SQLite-backed indexstore implementation. Construct via
// New. Methods are safe for concurrent use; the underlying SQLite
// handle is serialised by harmonysqlite's connection.
type Store struct {
	db *harmonysqlite.DB
}

// New wraps a *harmonysqlite.DB into a Store. The DB is expected to
// have the indexstore schema applied (schema-curio-core/0015_indexstore.sql).
// harmonysqlite.New applies migrations automatically; callers that
// constructed the DB any other way must apply the schema explicitly.
func New(db *harmonysqlite.DB) *Store {
	return &Store{db: db}
}

// AddPDPLayer inserts a precomputed Merkle layer for a piece. Each
// NodeDigest carries (Layer, Index, Hash); the layer index is shared
// across all entries in the slice but stored per row for query symmetry.
//
// Insertions are wrapped in a single transaction for atomic bulk-write
// semantics (matches upstream's `gocql.UnloggedBatch` shape; SQLite's
// transactional INSERT is the equivalent).
func (s *Store) AddPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layer []NodeDigest) error {
	if len(layer) == 0 {
		return fmt.Errorf("sqliteindex: AddPDPLayer: no records to insert")
	}
	pieceCidBytes := pieceCidV2.Bytes()

	_, err := s.db.BeginTransaction(ctx, func(tx *harmonysqlite.Tx) (bool, error) {
		for _, r := range layer {
			if _, err := tx.Exec(`
				INSERT INTO pdp_cache_layer (piece_cid, layer_index, leaf_index, leaf)
				VALUES (?, ?, ?, ?)
				ON CONFLICT (piece_cid, layer_index, leaf_index) DO UPDATE SET leaf = excluded.leaf`,
				pieceCidBytes, r.Layer, r.Index, r.Hash[:]); err != nil {
				return false, fmt.Errorf("sqliteindex: insert pdp_cache_layer (piece %s layer %d leaf %d): %w",
					pieceCidV2.String(), r.Layer, r.Index, err)
			}
		}
		return true, nil
	})
	return err
}

// GetPDPLayerIndex returns (has, layerIdx, nil) where layerIdx is the
// cached layer index for the piece, or (false, 0, nil) if no row exists.
//
// Upstream Cassandra uses SELECT ... LIMIT 1 without ordering; per-piece
// there is exactly one (layer_index) value because task_save_cache
// writes one layer per piece. We preserve that semantic with LIMIT 1.
func (s *Store) GetPDPLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error) {
	var layerIdx int
	row := s.db.QueryRow(ctx,
		`SELECT layer_index FROM pdp_cache_layer WHERE piece_cid = ? LIMIT 1`,
		pieceCidV2.Bytes())
	err := row.Scan(&layerIdx)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, 0, nil
	case err != nil:
		return false, 0, fmt.Errorf("sqliteindex: GetPDPLayerIndex piece %s: %w", pieceCidV2.String(), err)
	}
	return true, layerIdx, nil
}

// GetPDPLayer returns all NodeDigest entries for (piece, layer), sorted
// by leaf index ascending. Matches upstream's PageSize(2000) iteration
// shape; SQLite reads the whole result set in one query since the
// expected layer size for hot-storage pieces is bounded (typically
// <= 64 KiB worth of leaves for a 32 GiB piece's cached layer).
func (s *Store) GetPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]NodeDigest, error) {
	rows, err := s.db.Query(ctx,
		`SELECT leaf_index, leaf FROM pdp_cache_layer WHERE piece_cid = ? AND layer_index = ? ORDER BY leaf_index ASC`,
		pieceCidV2.Bytes(), layerIdx)
	if err != nil {
		return nil, fmt.Errorf("sqliteindex: GetPDPLayer query: %w", err)
	}
	defer rows.Close()

	var out []NodeDigest
	for rows.Next() {
		var (
			leafIdx int64
			leaf    []byte
		)
		if err := rows.Scan(&leafIdx, &leaf); err != nil {
			return nil, fmt.Errorf("sqliteindex: GetPDPLayer scan: %w", err)
		}
		if len(leaf) != 32 {
			return nil, fmt.Errorf("sqliteindex: GetPDPLayer leaf %d: expected 32-byte digest, got %d", leafIdx, len(leaf))
		}
		var hash [32]byte
		copy(hash[:], leaf)
		out = append(out, NodeDigest{
			Layer: layerIdx,
			Index: leafIdx,
			Hash:  hash,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqliteindex: GetPDPLayer rows: %w", err)
	}

	// Defensive: ORDER BY leaf_index ASC should already produce sorted
	// output, but the upstream impl also sorts client-side. Match it.
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// GetPDPNode returns a single (layer, index) leaf for a piece.
// Returns (false, nil, nil) when the leaf is absent.
func (s *Store) GetPDPNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, *NodeDigest, error) {
	var leaf []byte
	row := s.db.QueryRow(ctx,
		`SELECT leaf FROM pdp_cache_layer WHERE piece_cid = ? AND layer_index = ? AND leaf_index = ? LIMIT 1`,
		pieceCidV2.Bytes(), layerIdx, index)
	err := row.Scan(&leaf)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil, nil
	case err != nil:
		return false, nil, fmt.Errorf("sqliteindex: GetPDPNode piece %s layer %d leaf %d: %w",
			pieceCidV2.String(), layerIdx, index, err)
	}
	if len(leaf) != 32 {
		return false, nil, fmt.Errorf("sqliteindex: GetPDPNode leaf %d: expected 32-byte digest, got %d", index, len(leaf))
	}
	var hash [32]byte
	copy(hash[:], leaf)
	return true, &NodeDigest{Layer: layerIdx, Index: index, Hash: hash}, nil
}

// RemoveIndexes drops all rows for a piece from pdp_cache_layer. The
// upstream impl also removes from PieceBlockOffsetSize + PayloadToPieces
// (the IPNI indexing tables); curio-core doesn't populate those, so the
// upstream semantic of "best-effort removal across all index tables for
// this piece" reduces to "drop the cache layer rows."
func (s *Store) RemoveIndexes(ctx context.Context, pieceCidV2 cid.Cid) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM pdp_cache_layer WHERE piece_cid = ?`,
		pieceCidV2.Bytes())
	if err != nil {
		return fmt.Errorf("sqliteindex: RemoveIndexes piece %s: %w", pieceCidV2.String(), err)
	}
	return nil
}

// FindPieceInAggregate is the mk20 aggregate-piece lookup used by
// cachedreader's getPieceReaderFromAggregate path. curio-core is
// pdpv0-only (mk20 is explicitly out of scope, per Andy 2026-05-23),
// so piece_by_aggregate is never populated. The query returns empty,
// cachedreader cleanly falls through to the piecePark path which is
// what we actually want for pdpv0 pieces.
//
// Implemented as a real query against the (empty) table rather than a
// hardcoded nil so the interface contract holds — any future caller
// that populates the table works without code changes here.
func (s *Store) FindPieceInAggregate(ctx context.Context, pieceCid cid.Cid) ([]Record, error) {
	rows, err := s.db.Query(ctx, `
		SELECT aggregate_piece_cid, unpadded_offset, unpadded_length
		FROM piece_by_aggregate
		WHERE piece_cid = ?
		ORDER BY aggregate_piece_cid, unpadded_offset`,
		pieceCid.Bytes())
	if err != nil {
		return nil, fmt.Errorf("sqliteindex: FindPieceInAggregate query: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var (
			aggBytes    []byte
			off, length int64
		)
		if err := rows.Scan(&aggBytes, &off, &length); err != nil {
			return nil, fmt.Errorf("sqliteindex: FindPieceInAggregate scan: %w", err)
		}
		aggCid, _, err := decodePieceCid(aggBytes)
		if err != nil {
			return nil, fmt.Errorf("sqliteindex: FindPieceInAggregate aggregate cid: %w", err)
		}
		// Upstream Record shape: Cid + Offset + Size. The aggregate
		// piece CID goes into Cid; offset + size describe where this
		// piece sits within that aggregate.
		out = append(out, Record{
			Cid:    aggCid,
			Offset: uint64(off),
			Size:   uint64(length),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqliteindex: FindPieceInAggregate rows: %w", err)
	}
	return out, nil
}

// decodePieceCid parses a CID from raw bytes (the on-disk shape
// matching `cid.Bytes()`). Returns the CID + the unread tail (zero
// here; cid.CidFromBytes returns the consumed length).
func decodePieceCid(b []byte) (cid.Cid, int, error) {
	n, c, err := cid.CidFromBytes(b)
	if err != nil {
		return cid.Undef, 0, err
	}
	return c, n, nil
}

// Compile-time guards: package depends on bytes for an unused import-
// suppression import; the runtime path doesn't reference it.
// *Store satisfies indexstore.Backend (the contract defined upstream).
var _ = bytes.NewReader
var _ indexstore.Backend = (*Store)(nil)
