package config

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func newTestDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{
		Path:        ":memory:",
		ForeignKeys: true,
	})
	if err != nil {
		t.Fatalf("harmonysqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestStatus_FreshDB asserts a never-configured DB reports
// NeedsSetup=true and lists every required field in canonical order.
func TestStatus_FreshDB(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	st, err := Status(ctx, db)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.NeedsSetup {
		t.Error("Status.NeedsSetup = false on a fresh DB; want true")
	}
	want := []string{FieldMarketAddress, FieldWalletAddress, FieldMinerID}
	if !reflect.DeepEqual(st.Missing, want) {
		t.Errorf("Status.Missing = %v, want %v", st.Missing, want)
	}
}

// TestStatus_PartialBundle asserts that an existing row with some
// empty fields still flags NeedsSetup=true and names only the empty
// fields.
func TestStatus_PartialBundle(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// Directly insert a partial bundle (UpsertDefaultLayer would
	// reject it).
	body, err := encodeBundle(ConfigBundle{
		Pdp: PdpSection{
			MarketAddress: "0xMARKET",
			// WalletAddress + MinerID empty
		},
	})
	if err != nil {
		t.Fatalf("encodeBundle: %v", err)
	}
	if _, err := db.Exec(ctx,
		`INSERT INTO harmony_config (title, config) VALUES (?, ?)`,
		DefaultLayerName, body); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := Status(ctx, db)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.NeedsSetup {
		t.Error("Status.NeedsSetup = false on partial bundle; want true")
	}
	want := []string{FieldWalletAddress, FieldMinerID}
	if !reflect.DeepEqual(st.Missing, want) {
		t.Errorf("Status.Missing = %v, want %v", st.Missing, want)
	}
}

// TestUpsertDefaultLayer_RoundTrip asserts the happy-path: write a
// complete bundle, then Status reports NeedsSetup=false.
func TestUpsertDefaultLayer_RoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	cfg := ConfigBundle{
		Pdp: PdpSection{
			MarketAddress: "0xMARKET",
			WalletAddress: "0xWALLET",
			MinerID:       "f01234",
		},
	}
	if err := UpsertDefaultLayer(ctx, db, cfg); err != nil {
		t.Fatalf("UpsertDefaultLayer: %v", err)
	}

	st, err := Status(ctx, db)
	if err != nil {
		t.Fatalf("Status after Upsert: %v", err)
	}
	if st.NeedsSetup {
		t.Errorf("Status.NeedsSetup = true after Upsert; want false (missing=%v)", st.Missing)
	}
	if len(st.Missing) != 0 {
		t.Errorf("Status.Missing = %v, want empty slice", st.Missing)
	}

	// Second upsert replaces (no PK conflict error).
	cfg.Pdp.MinerID = "f099999"
	if err := UpsertDefaultLayer(ctx, db, cfg); err != nil {
		t.Errorf("second UpsertDefaultLayer: %v", err)
	}

	// Verify the new value is the one that took effect.
	row := db.QueryRow(ctx, `SELECT config FROM harmony_config WHERE title = ?`, DefaultLayerName)
	var blob string
	if err := row.Scan(&blob); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !strings.Contains(blob, "f099999") {
		t.Errorf("default layer body does not contain updated MinerID; got:\n%s", blob)
	}
}

// TestUpsertDefaultLayer_RejectsEmptyFields asserts validation fires
// before any DB write.
func TestUpsertDefaultLayer_RejectsEmptyFields(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	cases := []struct {
		name string
		cfg  ConfigBundle
		want string // substring expected in error
	}{
		{"all empty", ConfigBundle{}, "market_address"},
		{"missing wallet", ConfigBundle{
			Pdp: PdpSection{MarketAddress: "x", MinerID: "y"},
		}, "wallet_address"},
		{"whitespace-only", ConfigBundle{
			Pdp: PdpSection{MarketAddress: "  ", WalletAddress: "x", MinerID: "y"},
		}, "market_address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := UpsertDefaultLayer(ctx, db, tc.cfg)
			if err == nil {
				t.Fatal("UpsertDefaultLayer returned nil; want validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want substring %q", err, tc.want)
			}
		})
	}

	// No row should have been written.
	row := db.QueryRow(ctx, `SELECT count(*) FROM harmony_config WHERE title = ?`, DefaultLayerName)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 0 {
		t.Errorf("harmony_config row count = %d after rejected upserts, want 0", n)
	}
}

// TestEncodeDecode_RoundTrip is a small sanity check on the TOML
// shape (catches accidental tag drift).
func TestEncodeDecode_RoundTrip(t *testing.T) {
	in := ConfigBundle{
		Pdp: PdpSection{
			MarketAddress: "0xA",
			WalletAddress: "0xB",
			MinerID:       "f0C",
		},
	}
	blob, err := encodeBundle(in)
	if err != nil {
		t.Fatalf("encodeBundle: %v", err)
	}
	out, err := decodeBundle(blob)
	if err != nil {
		t.Fatalf("decodeBundle: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round-trip mismatch:\nin:  %+v\nout: %+v\ntoml:\n%s", in, out, blob)
	}
}
