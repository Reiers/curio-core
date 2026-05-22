package harmonysqlite

import (
	"context"
	"strings"
	"testing"
)

// TestApplyMigrations_FreshDatabase: a clean database goes through
// every embedded migration, the bookkeeping table records all of them,
// and a re-run is a no-op.
func TestApplyMigrations_FreshDatabase(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	// Verify the bookkeeping table got populated.
	files, err := MigrationFiles()
	if err != nil {
		t.Fatalf("MigrationFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no embedded migrations found")
	}
	rows, err := db.Query(ctx, `SELECT name FROM harmony_schema_migrations ORDER BY name`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got = append(got, n)
	}
	if len(got) != len(files) {
		t.Errorf("applied %d migrations, expected %d (files: %v, applied: %v)",
			len(got), len(files), files, got)
	}

	// Idempotency: rerun.
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("re-run ApplyMigrations: %v", err)
	}
}

// TestApplyMigrations_CoreTablesPresent: after applying, harmony_machines /
// harmony_task / message_sends / message_waits / harmony_config all exist
// and have the columns we'd expect to query later.
func TestApplyMigrations_CoreTablesPresent(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	expectedTables := []string{
		"harmony_machines",
		"harmony_task",
		"harmony_task_history",
		"harmony_task_follow",
		"harmony_task_impl",
		"harmony_task_singletons",
		"harmony_machine_details",
		"message_sends",
		"message_send_locks",
		"message_waits",
		"harmony_config",
		"harmony_schema_migrations",
	}
	for _, tbl := range expectedTables {
		t.Run(tbl, func(t *testing.T) {
			var nameOut string
			err := db.QueryRow(ctx,
				`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&nameOut)
			if err != nil {
				t.Errorf("table %s missing: %v", tbl, err)
			}
		})
	}
}

// TestApplyMigrations_InsertSampleRow: exercise the post-migration
// schema by inserting one row into the load-bearing tables and reading
// back via the higher-level API.
func TestApplyMigrations_InsertSampleRow(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	// Insert a machine + a task with that owner.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
		 VALUES (?, ?, ?, ?)`,
		"127.0.0.1:1234", 8, 16*1024*1024*1024, 0.0); err != nil {
		t.Fatalf("INSERT harmony_machines: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO harmony_task (posted_time, owner_id, added_by, name)
		 VALUES (CURRENT_TIMESTAMP, 1, 1, ?)`,
		"pdp-prove"); err != nil {
		t.Fatalf("INSERT harmony_task: %v", err)
	}

	type machineRow struct {
		ID          int64  `db:"id"`
		HostAndPort string `db:"host_and_port"`
		CPU         int    `db:"cpu"`
	}
	var machines []machineRow
	if err := db.Select(ctx, &machines,
		`SELECT id, host_and_port, cpu FROM harmony_machines`); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(machines) != 1 || machines[0].HostAndPort != "127.0.0.1:1234" || machines[0].CPU != 8 {
		t.Errorf("Select returned %+v, expected one row with 127.0.0.1:1234 / 8 CPU", machines)
	}

	// And the task references back via FK.
	var taskOwner int
	if err := db.QueryRow(ctx,
		`SELECT owner_id FROM harmony_task WHERE name = ?`, "pdp-prove").Scan(&taskOwner); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if taskOwner != int(machines[0].ID) {
		t.Errorf("task owner_id %d != machine id %d", taskOwner, machines[0].ID)
	}
}

// TestApplyMigrations_PDPTablesPresent: after applying, the PDP-domain
// tables (v1 + v0 vocabulary), the eth chain queue, IPNI tables, and the
// mk20 deal table are all there. This is the Day-3 assertion that the
// SQL classification + translation actually produced the schema PDP
// would need at runtime.
func TestApplyMigrations_PDPTablesPresent(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	wantTables := []string{
		// Storage layer
		"parked_pieces", "parked_piece_refs",
		"sector_location", "storage_path",
		// Eth chain queue
		"eth_keys", "message_sends_eth", "message_waits_eth", "message_send_eth_locks",
		// PDP v1
		"pdp_services", "pdp_piecerefs", "pdp_piece_uploads", "pdp_piece_mh_to_commp",
		"pdp_proof_sets", "pdp_proofset_creates", "pdp_proofset_roots", "pdp_proofset_root_adds",
		"pdp_prove_tasks",
		// PDP v0 (renamed vocabulary)
		"pdp_data_sets", "pdp_data_set_creates", "pdp_data_set_pieces", "pdp_data_set_piece_adds",
		"pdp_piece_streaming_uploads", "pdp_piece_pulls", "pdp_piece_pull_items",
		"filecoin_payment_transactions",
		// IPNI (pdpv0 advertises)
		"ipni", "ipni_head", "ipni_peerid", "ipni_chunks", "ipni_ad_fetches",
		// mk20
		"market_mk20_deal", "market_piece_deal", "ddo_contracts",
	}
	for _, tbl := range wantTables {
		t.Run(tbl, func(t *testing.T) {
			var got string
			err := db.QueryRow(ctx,
				`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&got)
			if err != nil {
				t.Errorf("table %s missing: %v", tbl, err)
			}
		})
	}
}

// TestApplyMigrations_PDPRefcountTriggerWorks: the inline-body trigger
// pdp_data_set_piece_insert (a SQLite replacement for the upstream PG
// function increment_data_set_refcount) increments the right column on
// pdp_piecerefs when a pdp_data_set_pieces row is inserted.
func TestApplyMigrations_PDPRefcountTriggerWorks(t *testing.T) {
	db, err := Open(Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	// Seed the dependency chain: service → parked piece → piece ref →
	// pdp_pieceref → data set + message wait → data set piece.
	if _, err := db.ExecCount(ctx,
		`INSERT INTO pdp_services (service_label, pubkey) VALUES (?, ?)`,
		"test", []byte("deadbeef")); err != nil {
		t.Fatalf("INSERT service: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size) VALUES (?, ?, ?)`,
		"bafyfoo", 1024, 1000); err != nil {
		t.Fatalf("INSERT parked_pieces: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO parked_piece_refs (piece_id) VALUES (1)`); err != nil {
		t.Fatalf("INSERT parked_piece_refs: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref) VALUES (?, ?, 1)`,
		"test", "bafyfoo"); err != nil {
		t.Fatalf("INSERT pdp_piecerefs: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO pdp_data_sets (id, create_message_hash, service) VALUES (?, ?, ?)`,
		1, "0xhash", "test"); err != nil {
		t.Fatalf("INSERT pdp_data_sets: %v", err)
	}
	if _, err := db.ExecCount(ctx,
		`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES (?, 'pending')`,
		"0xhash"); err != nil {
		t.Fatalf("INSERT message_waits_eth: %v", err)
	}
	// pre-trigger refcount is 0
	var refcount int
	if err := db.QueryRow(ctx,
		`SELECT data_set_refcount FROM pdp_piecerefs WHERE id = 1`).Scan(&refcount); err != nil {
		t.Fatalf("read refcount: %v", err)
	}
	if refcount != 0 {
		t.Errorf("pre-insert refcount = %d, want 0", refcount)
	}
	// insert a pdp_data_set_pieces row → should bump data_set_refcount
	if _, err := db.ExecCount(ctx, `
		INSERT INTO pdp_data_set_pieces
		  (data_set, piece, add_message_hash, add_message_index,
		   piece_id, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
		VALUES (1, 'bafyfoo', '0xhash', 0, 0, 'bafyfoo', 0, 1024, 1)`); err != nil {
		t.Fatalf("INSERT pdp_data_set_pieces: %v", err)
	}
	if err := db.QueryRow(ctx,
		`SELECT data_set_refcount FROM pdp_piecerefs WHERE id = 1`).Scan(&refcount); err != nil {
		t.Fatalf("read refcount post-trigger: %v", err)
	}
	if refcount != 1 {
		t.Errorf("post-insert refcount = %d, want 1 (the inline trigger should have bumped it)", refcount)
	}
}

// TestApplyMigrations_FKEnforced: foreign keys are ON, so inserting a
// task with a bogus owner_id fails.
func TestApplyMigrations_FKEnforced(t *testing.T) {
	db, err := Open(Config{Path: ":memory:", ForeignKeys: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	_, err = db.ExecCount(ctx,
		`INSERT INTO harmony_task (posted_time, owner_id, added_by, name)
		 VALUES (CURRENT_TIMESTAMP, 99999, 1, ?)`,
		"orphan-task")
	if err == nil {
		t.Error("expected FK violation, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "FOREIGN KEY") && !strings.Contains(err.Error(), "constraint failed") {
		// SQLite returns "FOREIGN KEY constraint failed"; some build options
		// may phrase it differently. Accept any error pointing at constraints.
		t.Logf("FK violation (formatted): %v", err)
	}
}
