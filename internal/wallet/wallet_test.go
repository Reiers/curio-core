// wallet_test.go — unit tests for internal/wallet. Uses an in-memory
// SQLite database so the test suite stays hermetic + fast.

package wallet_test

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
	"github.com/Reiers/curio-core/internal/wallet"
)

func setupDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.New(context.Background(), harmonysqlite.Config{
		Path: ":memory:",
	})
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNew_GeneratesUniqueAddresses(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	a1, err := wallet.New(ctx, db, "pdp")
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	a2, err := wallet.New(ctx, db, "backup")
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	if a1 == a2 {
		t.Fatalf("expected unique addresses, got %s twice", a1.Hex())
	}
}

func TestImport_RoundTripsExport(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	// Generate an arbitrary key OUTSIDE the wallet package + import it.
	priv, _ := crypto.GenerateKey()
	rawKey := crypto.FromECDSA(priv)
	expectedAddr := crypto.PubkeyToAddress(priv.PublicKey)

	imported, err := wallet.Import(ctx, db, common.Bytes2Hex(rawKey), "operator")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported != expectedAddr {
		t.Fatalf("Import derived %s, want %s", imported.Hex(), expectedAddr.Hex())
	}

	exported, err := wallet.Export(ctx, db, imported)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if common.Bytes2Hex(exported) != common.Bytes2Hex(rawKey) {
		t.Fatalf("Export round-trip mismatch:\n got %s\nwant %s",
			common.Bytes2Hex(exported), common.Bytes2Hex(rawKey))
	}
}

func TestImport_RefusesDuplicate(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	priv, _ := crypto.GenerateKey()
	rawKey := common.Bytes2Hex(crypto.FromECDSA(priv))

	if _, err := wallet.Import(ctx, db, rawKey, "pdp"); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	if _, err := wallet.Import(ctx, db, rawKey, "operator"); err == nil {
		t.Fatalf("expected second Import of same key to fail")
	}
}

func TestImport_BadHex(t *testing.T) {
	db := setupDB(t)
	cases := []string{"", "0x", "0xZZ", "0x01"} // short, junk, too-short
	for _, c := range cases {
		if _, err := wallet.Import(context.Background(), db, c, "pdp"); err == nil {
			t.Errorf("Import(%q) expected error, got nil", c)
		}
	}
}

func TestList_ReturnsSortedByRoleThenAddress(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	addrs := make([]common.Address, 3)
	roles := []string{"pdp", "backup", "operator"}
	for i, role := range roles {
		a, err := wallet.New(ctx, db, role)
		if err != nil {
			t.Fatalf("New(%s): %v", role, err)
		}
		addrs[i] = a
	}

	rows, err := wallet.List(ctx, db)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	// Roles should be alphabetical: backup, operator, pdp
	if rows[0].Role != "backup" || rows[1].Role != "operator" || rows[2].Role != "pdp" {
		t.Errorf("rows not sorted by role: %+v", rows)
	}
}

func TestDelete_RefusesLastPDP(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	addr, err := wallet.New(ctx, db, "pdp")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := wallet.Delete(ctx, db, addr); err == nil {
		t.Fatalf("expected Delete of last pdp wallet to fail")
	}

	// After adding a second pdp row, Delete should succeed on the first.
	_, _ = wallet.New(ctx, db, "pdp")
	removed, err := wallet.Delete(ctx, db, addr)
	if err != nil {
		t.Fatalf("Delete with multiple pdp rows: %v", err)
	}
	if !removed {
		t.Fatalf("expected Delete to remove the row")
	}
}

func TestSetRole_UpdatesExistingRow(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	addr, err := wallet.New(ctx, db, "pdp")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := wallet.SetRole(ctx, db, addr, "backup"); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	row, ok, err := wallet.Get(ctx, db, addr)
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if row.Role != "backup" {
		t.Errorf("role = %q, want backup", row.Role)
	}
}

func TestValidateRole_RejectsBadValues(t *testing.T) {
	db := setupDB(t)
	bad := []string{"", "with space", "with/slash", "with.dot", "very-long-role-name-that-exceeds-the-32-character-limit"}
	for _, r := range bad {
		if _, err := wallet.New(context.Background(), db, r); err == nil {
			t.Errorf("expected role %q to be rejected", r)
		}
	}
}
