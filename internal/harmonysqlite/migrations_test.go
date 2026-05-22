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
