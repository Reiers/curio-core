package alerts

import (
	"context"
	"strings"
	"testing"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

func newTestDB(t *testing.T) *harmonysqlite.DB {
	t.Helper()
	db, err := harmonysqlite.Open(harmonysqlite.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("open in-memory DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Apply just the alerts table directly; we don't need the full
	// schema chain for these unit tests.
	if _, err := db.ExecCount(context.Background(), `
		CREATE TABLE curio_alerts (
		    id INTEGER PRIMARY KEY AUTOINCREMENT,
		    fingerprint TEXT NOT NULL UNIQUE,
		    severity TEXT NOT NULL,
		    source TEXT NOT NULL,
		    message TEXT NOT NULL,
		    context_json TEXT NOT NULL DEFAULT '{}',
		    first_seen_at INTEGER NOT NULL,
		    last_seen_at INTEGER NOT NULL,
		    count INTEGER NOT NULL DEFAULT 1,
		    acked INTEGER NOT NULL DEFAULT 0,
		    acked_at INTEGER
		)
	`); err != nil {
		t.Fatalf("create curio_alerts: %v", err)
	}
	return db
}

func TestEmit_BasicAndDedup(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// First emit creates a row, count=1, acked=0.
	id1, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "first failure",
		Context:  map[string]any{"task_id": int64(42)},
	})
	if err != nil {
		t.Fatalf("first emit: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero id from first emit")
	}

	// Second emit with same params dedupes: same id, count=2.
	id2, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "first failure",
		Context:  map[string]any{"task_id": int64(42)},
	})
	if err != nil {
		t.Fatalf("second emit: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected dedup to return same id; got id1=%d id2=%d", id1, id2)
	}

	rows, err := Recent(ctx, db, 100, false)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after dedup; got %d", len(rows))
	}
	if rows[0].Count != 2 {
		t.Fatalf("expected count=2 after dedup; got %d", rows[0].Count)
	}
	if rows[0].Acked != 0 {
		t.Fatalf("expected acked=0 after re-emit; got %d", rows[0].Acked)
	}
	if got := rows[0].Context["task_id"]; got == nil {
		t.Fatalf("expected task_id in decoded context; got %#v", rows[0].Context)
	}
}

func TestEmit_DistinctFingerprintsCreateDistinctRows(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if _, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "task 42 failed",
		Context:  map[string]any{"task_id": int64(42)},
	}); err != nil {
		t.Fatalf("first emit: %v", err)
	}
	if _, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "task 43 failed",
		Context:  map[string]any{"task_id": int64(43)},
	}); err != nil {
		t.Fatalf("second emit: %v", err)
	}

	rows, err := Recent(ctx, db, 100, false)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 distinct rows; got %d", len(rows))
	}
}

func TestAck(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	id, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityError,
		Source:   "test/unit",
		Message:  "boom",
		Context:  map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	n, err := Ack(ctx, db, id)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected ack to change 1 row; got %d", n)
	}

	// Re-acking is a no-op.
	n, err = Ack(ctx, db, id)
	if err != nil {
		t.Fatalf("re-ack: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected re-ack to change 0 rows; got %d", n)
	}

	// Verify it's filtered by Recent(onlyUnacked=true).
	rows, err := Recent(ctx, db, 100, true)
	if err != nil {
		t.Fatalf("Recent unacked: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 unacked rows; got %d", len(rows))
	}

	// But Recent(onlyUnacked=false) still shows it.
	rows, err = Recent(ctx, db, 100, false)
	if err != nil {
		t.Fatalf("Recent all: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row total; got %d", len(rows))
	}
	if rows[0].Acked != 1 {
		t.Fatalf("expected acked=1; got %d", rows[0].Acked)
	}
}

func TestEmit_ReEmitClearsAck(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	id, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "flapping",
		Context:  map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("emit 1: %v", err)
	}
	if _, err := Ack(ctx, db, id); err != nil {
		t.Fatalf("ack: %v", err)
	}

	// Re-emitting the same fingerprint clears the ack (operator should
	// see the alert come back when it recurs after they ack'd it).
	if _, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityWarning,
		Source:   "test/unit",
		Message:  "flapping",
		Context:  map[string]any{"k": "v"},
	}); err != nil {
		t.Fatalf("re-emit: %v", err)
	}

	rows, err := Recent(ctx, db, 100, true)
	if err != nil {
		t.Fatalf("Recent unacked after re-emit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 unacked row after re-emit; got %d", len(rows))
	}
	if rows[0].Count != 2 {
		t.Fatalf("expected count=2; got %d", rows[0].Count)
	}
}

func TestFingerprintStability(t *testing.T) {
	a := Fingerprint("src", map[string]any{"a": 1, "b": 2})
	b := Fingerprint("src", map[string]any{"b": 2, "a": 1})
	if a != b {
		t.Fatalf("Fingerprint should be order-independent; got %s vs %s", a, b)
	}
	c := Fingerprint("src", map[string]any{"a": 1, "b": 3})
	if a == c {
		t.Fatal("Fingerprint should differ when param values differ")
	}
	d := Fingerprint("other", map[string]any{"a": 1, "b": 2})
	if a == d {
		t.Fatal("Fingerprint should differ when source differs")
	}
}

func TestCountsOf(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for i, sev := range []Severity{SeverityWarning, SeverityWarning, SeverityError, SeverityCritical} {
		if _, err := Emit(ctx, db, EmitArgs{
			Severity: sev,
			Source:   "test/counts",
			Message:  "msg",
			Context:  map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	c, err := CountsOf(ctx, db)
	if err != nil {
		t.Fatalf("CountsOf: %v", err)
	}
	if c.Total != 4 {
		t.Fatalf("expected Total=4; got %d", c.Total)
	}
	if c.Unacked != 4 {
		t.Fatalf("expected Unacked=4; got %d", c.Unacked)
	}
	if c.BySeverity["warning"] != 2 || c.BySeverity["error"] != 1 || c.BySeverity["critical"] != 1 {
		t.Fatalf("BySeverity mismatch: %#v", c.BySeverity)
	}
}

func TestDecodeContext_TrimEmpty(t *testing.T) {
	a := &Alert{ContextJSON: "   "}
	if err := a.DecodeContext(); err != nil {
		t.Fatalf("DecodeContext empty: %v", err)
	}
	if a.Context == nil {
		t.Fatal("expected non-nil Context after DecodeContext on empty input")
	}
	if len(a.Context) != 0 {
		t.Fatalf("expected empty Context; got %#v", a.Context)
	}
}

func TestEmit_MessageTruncation(t *testing.T) {
	// Just a sanity check that long messages survive the round trip.
	long := strings.Repeat("x", 5000)
	ctx := context.Background()
	db := newTestDB(t)
	if _, err := Emit(ctx, db, EmitArgs{
		Severity: SeverityError,
		Source:   "test/long",
		Message:  long,
		Context:  map[string]any{},
	}); err != nil {
		t.Fatalf("emit long: %v", err)
	}
	rows, _ := Recent(ctx, db, 1, false)
	if len(rows) == 0 || rows[0].Message != long {
		t.Fatalf("expected long message to round-trip cleanly")
	}
}
