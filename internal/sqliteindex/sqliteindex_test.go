package sqliteindex

import (
	"context"
	"testing"

	"github.com/ipfs/go-cid"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// newTestStore opens an in-memory SQLite with the curio-core schema
// applied (which includes 0015_indexstore.sql), wraps it in a Store.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{
		Path:        ":memory:",
		ForeignKeys: true,
	})
	if err != nil {
		t.Fatalf("harmonysqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

// testPieceCidV2 returns a deterministic PieceCIDv2 for tests. The
// shape (multihash code 0x1011 = fr32-sha2-256-trunc254-padbintree) is
// what upstream's ParsePieceCidV2 accepts.
func testPieceCidV2(t *testing.T) cid.Cid {
	t.Helper()
	// PieceCIDv2 fixture from upstream pdp/piece_cid_test.go.
	c, err := cid.Decode("bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}
	return c
}

// digest returns a deterministic 32-byte digest for the given index.
func digest(i int64) [32]byte {
	var h [32]byte
	// Spread the index across the digest so different leaves don't share
	// any bytes in test assertions.
	for j := 0; j < 32; j++ {
		h[j] = byte(i + int64(j))
	}
	return h
}

// TestAddAndGetPDPNode_RoundTrip exercises the canonical write+read path
// that ProveTask depends on: AddPDPLayer writes a layer of leaves,
// GetPDPNode reads back a single leaf, the bytes match.
func TestAddAndGetPDPNode_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	layer := []NodeDigest{
		{Layer: 5, Index: 0, Hash: digest(100)},
		{Layer: 5, Index: 1, Hash: digest(101)},
		{Layer: 5, Index: 2, Hash: digest(102)},
	}
	if err := s.AddPDPLayer(ctx, pcid, layer); err != nil {
		t.Fatalf("AddPDPLayer: %v", err)
	}

	for _, want := range layer {
		has, got, err := s.GetPDPNode(ctx, pcid, want.Layer, want.Index)
		if err != nil {
			t.Errorf("GetPDPNode (idx %d): %v", want.Index, err)
			continue
		}
		if !has {
			t.Errorf("GetPDPNode (idx %d): not found", want.Index)
			continue
		}
		if got.Hash != want.Hash {
			t.Errorf("GetPDPNode (idx %d): hash=%x want %x", want.Index, got.Hash, want.Hash)
		}
		if got.Layer != want.Layer || got.Index != want.Index {
			t.Errorf("GetPDPNode (idx %d): layer/index mismatch got=(%d,%d) want=(%d,%d)",
				want.Index, got.Layer, got.Index, want.Layer, want.Index)
		}
	}
}

// TestGetPDPNode_Absent: querying a leaf that was never written
// returns (false, nil, nil), not an error.
func TestGetPDPNode_Absent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)
	has, node, err := s.GetPDPNode(ctx, pcid, 0, 999)
	if err != nil {
		t.Fatalf("GetPDPNode (absent): %v", err)
	}
	if has || node != nil {
		t.Errorf("GetPDPNode (absent): got has=%v node=%v; want false, nil", has, node)
	}
}

// TestGetPDPLayer_OrderingAndCount: reading the full layer returns all
// leaves in ascending order by leaf index.
func TestGetPDPLayer_OrderingAndCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	// Insert in scrambled order.
	layer := []NodeDigest{
		{Layer: 3, Index: 5, Hash: digest(5)},
		{Layer: 3, Index: 0, Hash: digest(0)},
		{Layer: 3, Index: 9, Hash: digest(9)},
		{Layer: 3, Index: 3, Hash: digest(3)},
		{Layer: 3, Index: 1, Hash: digest(1)},
	}
	if err := s.AddPDPLayer(ctx, pcid, layer); err != nil {
		t.Fatalf("AddPDPLayer: %v", err)
	}

	got, err := s.GetPDPLayer(ctx, pcid, 3)
	if err != nil {
		t.Fatalf("GetPDPLayer: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("GetPDPLayer count=%d want 5", len(got))
	}
	wantOrder := []int64{0, 1, 3, 5, 9}
	for i, want := range wantOrder {
		if got[i].Index != want {
			t.Errorf("GetPDPLayer[%d].Index = %d want %d", i, got[i].Index, want)
		}
		if got[i].Hash != digest(want) {
			t.Errorf("GetPDPLayer[%d].Hash mismatch", i)
		}
	}
}

// TestGetPDPLayerIndex_Detection: returns (has=true, layerIdx, nil)
// when at least one row exists for the piece; (false, 0, nil) otherwise.
func TestGetPDPLayerIndex_Detection(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	// Fresh state: no rows.
	has, _, err := s.GetPDPLayerIndex(ctx, pcid)
	if err != nil {
		t.Fatalf("GetPDPLayerIndex (empty): %v", err)
	}
	if has {
		t.Error("GetPDPLayerIndex (empty): has=true want false")
	}

	// Insert one row at layer 7.
	if err := s.AddPDPLayer(ctx, pcid, []NodeDigest{{Layer: 7, Index: 0, Hash: digest(0)}}); err != nil {
		t.Fatalf("AddPDPLayer: %v", err)
	}
	has, layerIdx, err := s.GetPDPLayerIndex(ctx, pcid)
	if err != nil {
		t.Fatalf("GetPDPLayerIndex (after add): %v", err)
	}
	if !has {
		t.Error("GetPDPLayerIndex (after add): has=false want true")
	}
	if layerIdx != 7 {
		t.Errorf("GetPDPLayerIndex layerIdx=%d want 7", layerIdx)
	}
}

// TestRemoveIndexes_DropsAllRows: after RemoveIndexes, every method
// reports the piece as absent again.
func TestRemoveIndexes_DropsAllRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	if err := s.AddPDPLayer(ctx, pcid, []NodeDigest{
		{Layer: 2, Index: 0, Hash: digest(0)},
		{Layer: 2, Index: 1, Hash: digest(1)},
	}); err != nil {
		t.Fatalf("AddPDPLayer: %v", err)
	}

	if err := s.RemoveIndexes(ctx, pcid); err != nil {
		t.Fatalf("RemoveIndexes: %v", err)
	}

	has, _, _ := s.GetPDPLayerIndex(ctx, pcid)
	if has {
		t.Error("after RemoveIndexes: GetPDPLayerIndex has=true")
	}
	got, _ := s.GetPDPLayer(ctx, pcid, 2)
	if len(got) != 0 {
		t.Errorf("after RemoveIndexes: GetPDPLayer returned %d entries", len(got))
	}
}

// TestAddPDPLayer_Idempotent: re-inserting the same (piece, layer, idx)
// updates the leaf rather than failing on the composite PK.
func TestAddPDPLayer_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	if err := s.AddPDPLayer(ctx, pcid, []NodeDigest{
		{Layer: 0, Index: 0, Hash: digest(0)},
	}); err != nil {
		t.Fatalf("AddPDPLayer (first): %v", err)
	}

	// Re-insert with a different hash at the same key.
	if err := s.AddPDPLayer(ctx, pcid, []NodeDigest{
		{Layer: 0, Index: 0, Hash: digest(42)},
	}); err != nil {
		t.Fatalf("AddPDPLayer (replay): %v", err)
	}

	has, node, err := s.GetPDPNode(ctx, pcid, 0, 0)
	if err != nil || !has {
		t.Fatalf("GetPDPNode: has=%v err=%v", has, err)
	}
	if node.Hash != digest(42) {
		t.Errorf("idempotent replay didn't update leaf: got hash=%x", node.Hash)
	}
}

// TestAddPDPLayer_EmptyRejected: passing zero entries surfaces a
// caller error (matches upstream behaviour).
func TestAddPDPLayer_EmptyRejected(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	err := s.AddPDPLayer(ctx, pcid, nil)
	if err == nil {
		t.Error("AddPDPLayer(nil): expected error, got nil")
	}
}

// TestFindPieceInAggregate_EmptyTable: mk20 path; pdpv0 deployments
// never populate piece_by_aggregate. Query returns empty cleanly.
func TestFindPieceInAggregate_EmptyTable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	pcid := testPieceCidV2(t)

	records, err := s.FindPieceInAggregate(ctx, pcid)
	if err != nil {
		t.Fatalf("FindPieceInAggregate (empty): %v", err)
	}
	if len(records) != 0 {
		t.Errorf("FindPieceInAggregate (empty): got %d records, want 0", len(records))
	}
}

// TestMultiplePiecesIsolated: two pieces, two layers each; queries for
// one piece don't leak entries from the other.
func TestMultiplePiecesIsolated(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	pcidA := testPieceCidV2(t)
	// Build a second distinct CID by parsing a different upstream fixture.
	pcidB, err := cid.Decode("bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba")
	if err != nil {
		t.Fatalf("cid.Decode: %v", err)
	}

	if err := s.AddPDPLayer(ctx, pcidA, []NodeDigest{
		{Layer: 1, Index: 0, Hash: digest(0xa0)},
		{Layer: 1, Index: 1, Hash: digest(0xa1)},
	}); err != nil {
		t.Fatalf("AddPDPLayer A: %v", err)
	}
	if err := s.AddPDPLayer(ctx, pcidB, []NodeDigest{
		{Layer: 2, Index: 0, Hash: digest(0xb0)},
	}); err != nil {
		t.Fatalf("AddPDPLayer B: %v", err)
	}

	layerA, _ := s.GetPDPLayer(ctx, pcidA, 1)
	if len(layerA) != 2 {
		t.Errorf("piece A layer 1: got %d entries, want 2", len(layerA))
	}
	layerB, _ := s.GetPDPLayer(ctx, pcidB, 2)
	if len(layerB) != 1 {
		t.Errorf("piece B layer 2: got %d entries, want 1", len(layerB))
	}

	// Cross-checks: A's layer 2 is empty, B's layer 1 is empty.
	if got, _ := s.GetPDPLayer(ctx, pcidA, 2); len(got) != 0 {
		t.Errorf("piece A layer 2: got %d entries, want 0 (leaked from B)", len(got))
	}
	if got, _ := s.GetPDPLayer(ctx, pcidB, 1); len(got) != 0 {
		t.Errorf("piece B layer 1: got %d entries, want 0 (leaked from A)", len(got))
	}

	// RemoveIndexes A leaves B intact.
	if err := s.RemoveIndexes(ctx, pcidA); err != nil {
		t.Fatalf("RemoveIndexes A: %v", err)
	}
	if has, _, _ := s.GetPDPLayerIndex(ctx, pcidA); has {
		t.Error("RemoveIndexes A: piece A still present")
	}
	if has, _, _ := s.GetPDPLayerIndex(ctx, pcidB); !has {
		t.Error("RemoveIndexes A: piece B was inadvertently removed")
	}
}
