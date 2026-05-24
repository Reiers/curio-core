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

// TestSetField_PartialThenComplete asserts that setting fields one at
// a time via the CLI shape produces a complete config that Status()
// reports as ready.
func TestSetField_PartialThenComplete(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// Start fresh: Status should report all three missing.
	st, err := Status(ctx, db)
	if err != nil {
		t.Fatalf("Status (fresh): %v", err)
	}
	if !st.NeedsSetup || len(st.Missing) != 3 {
		t.Fatalf("fresh Status: %+v", st)
	}

	// Set fields one at a time; each step is a successful partial write.
	if err := SetField(ctx, db, "pdp.miner-id", "f03678816"); err != nil {
		t.Fatalf("SetField miner-id: %v", err)
	}
	if err := SetField(ctx, db, "pdp.wallet", "0xf73Aa7b26Cd1fd30A7D5039842E13A8C7344CfEe"); err != nil {
		t.Fatalf("SetField wallet: %v", err)
	}
	if err := SetField(ctx, db, "pdp.market", "0x02925630df557F957f70E112bA06e50965417CA0"); err != nil {
		t.Fatalf("SetField market: %v", err)
	}

	// Status now reports complete.
	st, err = Status(ctx, db)
	if err != nil {
		t.Fatalf("Status (after all 3): %v", err)
	}
	if st.NeedsSetup {
		t.Errorf("Status.NeedsSetup = true after all 3 set; want false (missing: %v)", st.Missing)
	}

	// ReadDefaultLayer round-trips the values.
	cfg, err := ReadDefaultLayer(ctx, db)
	if err != nil {
		t.Fatalf("ReadDefaultLayer: %v", err)
	}
	if cfg.Pdp.MinerID != "f03678816" {
		t.Errorf("MinerID = %q", cfg.Pdp.MinerID)
	}
	if cfg.Pdp.WalletAddress != "0xf73Aa7b26Cd1fd30A7D5039842E13A8C7344CfEe" {
		t.Errorf("WalletAddress = %q", cfg.Pdp.WalletAddress)
	}
	if cfg.Pdp.MarketAddress != "0x02925630df557F957f70E112bA06e50965417CA0" {
		t.Errorf("MarketAddress = %q", cfg.Pdp.MarketAddress)
	}
}

// TestSetField_AliasesAccepted covers the various field-name aliases
// the CLI accepts (pdp.miner-id, miner_id, minerid, etc).
func TestSetField_AliasesAccepted(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		alias string
		want  func(c ConfigBundle) string
	}{
		{"pdp.miner-id", func(c ConfigBundle) string { return c.Pdp.MinerID }},
		{"miner_id", func(c ConfigBundle) string { return c.Pdp.MinerID }},
		{"MINER-ID", func(c ConfigBundle) string { return c.Pdp.MinerID }},
		{"pdp.wallet", func(c ConfigBundle) string { return c.Pdp.WalletAddress }},
		{"wallet_address", func(c ConfigBundle) string { return c.Pdp.WalletAddress }},
		{"pdp.market", func(c ConfigBundle) string { return c.Pdp.MarketAddress }},
		{"market_address", func(c ConfigBundle) string { return c.Pdp.MarketAddress }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.alias, func(t *testing.T) {
			db := newTestDB(t)
			if err := SetField(ctx, db, c.alias, "TESTVAL"); err != nil {
				t.Fatalf("SetField %q: %v", c.alias, err)
			}
			cfg, err := ReadDefaultLayer(ctx, db)
			if err != nil {
				t.Fatalf("ReadDefaultLayer: %v", err)
			}
			if got := c.want(cfg); got != "TESTVAL" {
				t.Errorf("alias %q stored at wrong field; got %q", c.alias, got)
			}
		})
	}
}

// TestSetField_UnknownField asserts SetField rejects bad field names.
func TestSetField_UnknownField(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	err := SetField(ctx, db, "pdp.bogus", "anything")
	if err == nil {
		t.Fatal("SetField pdp.bogus: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown config field") {
		t.Errorf("error message %q; want 'unknown config field'", err.Error())
	}
}

// TestSetField_PreservesOtherFields asserts setting one field doesn't
// blank the others. (Read-mutate-write must preserve the rest.)
func TestSetField_PreservesOtherFields(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// Set all three.
	_ = SetField(ctx, db, "pdp.miner-id", "f0111")
	_ = SetField(ctx, db, "pdp.wallet", "0xWALLET")
	_ = SetField(ctx, db, "pdp.market", "0xMARKET")

	// Re-set just miner-id.
	if err := SetField(ctx, db, "pdp.miner-id", "f0222"); err != nil {
		t.Fatalf("SetField re-miner-id: %v", err)
	}
	cfg, _ := ReadDefaultLayer(ctx, db)
	if cfg.Pdp.MinerID != "f0222" {
		t.Errorf("MinerID = %q, want f0222", cfg.Pdp.MinerID)
	}
	if cfg.Pdp.WalletAddress != "0xWALLET" {
		t.Errorf("WalletAddress lost: %q", cfg.Pdp.WalletAddress)
	}
	if cfg.Pdp.MarketAddress != "0xMARKET" {
		t.Errorf("MarketAddress lost: %q", cfg.Pdp.MarketAddress)
	}
}
