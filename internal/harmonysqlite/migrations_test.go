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
		// PDP v1 (legacy pdp_proof_sets / pdp_proofset_* vocabulary was
		// renamed to pdp_data_sets / pdp_data_set_* and the old tables are
		// DROPPED by 0017_drop_legacy_v1_artifacts.sql — asserted absent below)
		"pdp_services", "pdp_piecerefs", "pdp_piece_uploads", "pdp_piece_mh_to_commp",
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

	// Legacy v1 vocabulary must be GONE after 0017_drop_legacy_v1_artifacts.
	droppedTables := []string{
		"pdp_proof_sets", "pdp_proofset_creates", "pdp_proofset_roots", "pdp_proofset_root_adds",
	}
	for _, tbl := range droppedTables {
		t.Run("dropped/"+tbl, func(t *testing.T) {
			var got string
			err := db.QueryRow(ctx,
				`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&got)
			if err == nil {
				t.Errorf("legacy table %s still present; 0017 should have dropped it", tbl)
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

// TestApplyMigrations_TaskCompletionDeleteWorks reproduces the live
// failure behind migration 0022: harmonytask's completion recorder does
// DELETE FROM harmony_task WHERE id=$1, and with foreign_keys=ON SQLite
// scans every table whose FK references harmony_task. When 0017 dropped
// pdp_proof_sets but left pdp_prove_tasks declaring
// REFERENCES pdp_proof_sets(id), that scan failed with
// "no such table: main.pdp_proof_sets" and EVERY task completion
// retried forever. This test fails if any FK in the schema dangles.
func TestApplyMigrations_TaskCompletionDeleteWorks(t *testing.T) {
	db, err := Open(Config{Path: ":memory:", ForeignKeys: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	// foreign_key_check catches dangling FK targets schema-wide.
	rows, err := db.Query(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_check errored (dangling FK table?): %v", err)
	}
	rows.Close()

	// And the actual harmonytask completion path: insert a task with no
	// owner, then DELETE it. The DELETE triggers FK-cascade scans across
	// all referencing tables (pdp_prove_tasks among them).
	var taskID int64
	err = db.QueryRow(ctx,
		`INSERT INTO harmony_task (posted_time, added_by, name)
		 VALUES (CURRENT_TIMESTAMP, 1, 'completion-test') RETURNING id`).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert harmony_task: %v", err)
	}
	if _, err := db.ExecCount(ctx, `DELETE FROM harmony_task WHERE id=?`, taskID); err != nil {
		t.Fatalf("DELETE FROM harmony_task failed (this is the 0022 bug class): %v", err)
	}

	// pdp_prove_tasks must be in the v0 shape: (data_set, task_id),
	// FK'd to pdp_data_sets, so task_prove.go's INSERT works.
	var colName string
	err = db.QueryRow(ctx,
		`SELECT name FROM pragma_table_info('pdp_prove_tasks') WHERE name='data_set'`).Scan(&colName)
	if err != nil {
		t.Errorf("pdp_prove_tasks.data_set column missing (still v1 'proofset' shape?): %v", err)
	}
}

// TestApplyMigrations_PiecerefsHarmonyTaskFKsAreSetNull is a structural
// guard for curio-core's mitigation of upstream Curio #1291 (PDPv0_IPNI
// serialization storm + stranded piecerefs). The stranded-pieceref half
// of that class was closed by declaring every FK column on pdp_piecerefs
// (and pdp_piece_uploads.notify_task_id) that references harmony_task
// with ON DELETE SET NULL, so a completed / deleted task cannot leave
// the row pinned to a dead task id.
//
// This test applies every embedded migration, reads back the FK shape
// via pragma_foreign_key_list, and fails if any of the load-bearing FK
// columns targeting harmony_task uses anything other than SET NULL.
//
// Adding a new task_id column on either table? Make it SET NULL (not
// CASCADE, not RESTRICT, not NO ACTION) and add it to the wantSetNull
// list below.
//
// Cross-refs: #87 (verify + document GA-blocker mitigations),
// filecoin-project/curio#1291.
func TestApplyMigrations_PiecerefsHarmonyTaskFKsAreSetNull(t *testing.T) {
	db, err := Open(Config{Path: ":memory:", ForeignKeys: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	// (table, column) pairs that MUST FK to harmony_task with
	// ON DELETE SET NULL. Each corresponds to a documented mitigation
	// in docs/concepts/scale-mitigations.md.
	wantSetNull := []struct {
		table  string
		column string
	}{
		{"pdp_piecerefs", "save_cache_task_id"},
		{"pdp_piecerefs", "indexing_task_id"},
		{"pdp_piecerefs", "ipni_task_id"},
		{"pdp_piece_uploads", "notify_task_id"},
	}

	for _, tc := range wantSetNull {
		tc := tc
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			// Confirm the column exists at all. Columns can vanish under a
			// bad rebase; better a clear error than a silent skip.
			var colName string
			err := db.QueryRow(ctx,
				`SELECT name FROM pragma_table_info(?) WHERE name = ?`,
				tc.table, tc.column).Scan(&colName)
			if err != nil {
				t.Fatalf("%s.%s missing from schema: %v", tc.table, tc.column, err)
			}

			// pragma_foreign_key_list columns:
			//   id, seq, table, from, to, on_update, on_delete, match.
			rows, err := db.Query(ctx,
				`SELECT [table], [from], on_delete
				   FROM pragma_foreign_key_list(?)
				  WHERE [from] = ?`,
				tc.table, tc.column)
			if err != nil {
				t.Fatalf("pragma_foreign_key_list(%s): %v", tc.table, err)
			}
			defer rows.Close()

			var (
				found    bool
				targetT  string
				onDelete string
			)
			for rows.Next() {
				var target, from, od string
				if err := rows.Scan(&target, &from, &od); err != nil {
					t.Fatalf("scan foreign_key_list row: %v", err)
				}
				if from != tc.column {
					continue
				}
				found = true
				targetT = target
				onDelete = od
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate foreign_key_list: %v", err)
			}

			if !found {
				t.Fatalf("%s.%s has no FK declared (mitigation for "+
					"filecoin-project/curio#1291 requires FK to harmony_task "+
					"with ON DELETE SET NULL, see #87)", tc.table, tc.column)
			}
			if targetT != "harmony_task" {
				t.Errorf("%s.%s FKs to %q, want harmony_task",
					tc.table, tc.column, targetT)
			}
			if onDelete != "SET NULL" {
				t.Errorf("%s.%s ON DELETE = %q, want \"SET NULL\" "+
					"(anything else can strand piecerefs on task delete, "+
					"see docs/concepts/scale-mitigations.md and #87)",
					tc.table, tc.column, onDelete)
			}
		})
	}
}
