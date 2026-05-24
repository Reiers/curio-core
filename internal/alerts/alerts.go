// Package alerts is the curio-core minimal alerts surface (Reiers/curio-core#48).
//
// Scope (V0):
//   - Emit alerts from pdpv0 tasks and infrastructure subsystems with dedup by
//     fingerprint, structured context, severity, and ack state.
//   - Read recent alerts and ack them via the `/admin/alerts` HTTP endpoint
//     (see internal/admin).
//
// Out of scope (for later, alongside #39 SP dashboard + future webhook config):
//   - Webhook delivery
//   - Severity routing policy
//   - Dashboard integration
//   - Alert history retention / rollup
//
// Design notes:
//
//   - Storage is the same SQLite database the rest of curio-core uses; no
//     separate alerts store. Migration 0019 creates the `curio_alerts` table.
//
//   - Dedup is fingerprint-keyed: the caller is responsible for choosing a
//     stable fingerprint that identifies "the same alert recurring" vs "a new
//     distinct alert". A good rule of thumb is `source + the smallest set of
//     params that make the alert actionable as a unit`. For example, a prove
//     failure on dataset 13977 piece 0 should dedup against itself, not against
//     a prove failure on dataset 13980.
//
//   - Emit is best-effort: if the DB write fails, we log a warning and move on
//     rather than failing the caller. Alerts are observability, not correctness.
//
//   - No external IO from Emit. Webhook fan-out, when added, will happen on a
//     separate task that reads from the alerts table.
package alerts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/curiostorage/harmonyquery"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("curio-core/alerts")

// Severity classifies the urgency of an alert. The vocabulary is free-form for
// now; we will firm it up if/when we wire alert routing.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Alert is the read-side representation of a row in curio_alerts.
type Alert struct {
	ID          int64    `db:"id" json:"id"`
	Fingerprint string   `db:"fingerprint" json:"fingerprint"`
	Severity    Severity `db:"severity" json:"severity"`
	Source      string   `db:"source" json:"source"`
	Message     string   `db:"message" json:"message"`
	ContextJSON string   `db:"context_json" json:"-"`
	FirstSeenAt int64    `db:"first_seen_at" json:"first_seen_at_ms"`
	LastSeenAt  int64    `db:"last_seen_at" json:"last_seen_at_ms"`
	Count       int64    `db:"count" json:"count"`
	Acked       int64    `db:"acked" json:"acked"`
	AckedAt     *int64   `db:"acked_at" json:"acked_at_ms,omitempty"`

	// Context is the decoded ContextJSON, populated by Recent/Get. Direct
	// callers reading the table should call DecodeContext themselves.
	Context map[string]any `json:"context"`
}

// DecodeContext decodes ContextJSON into Context. Safe to call on already
// decoded alerts (no-op when Context is non-nil).
func (a *Alert) DecodeContext() error {
	if a.Context != nil {
		return nil
	}
	a.Context = make(map[string]any)
	if strings.TrimSpace(a.ContextJSON) == "" {
		return nil
	}
	return json.Unmarshal([]byte(a.ContextJSON), &a.Context)
}

// Fingerprint computes a stable fingerprint from a source and a set of
// identifying parameters. Callers may also pass a fingerprint directly.
//
// The fingerprint is the SHA-256 of `source || "\x00" || sortedKey=value pairs`
// truncated to 16 bytes (32 hex chars). Sorting the params guarantees stability
// regardless of caller ordering.
func Fingerprint(source string, params map[string]any) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	h.Write([]byte(source))
	h.Write([]byte{0})
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%v\x00", k, params[k])
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// EmitArgs is the input to Emit. All fields are required unless noted.
type EmitArgs struct {
	// Severity classifies urgency. Defaults to SeverityWarning if empty.
	Severity Severity

	// Source identifies the subsystem or task emitting the alert, e.g.
	// "pdpv0/prove", "pdpv0/lifecycle", "sender_eth/dispatch".
	Source string

	// Message is the human-readable single-line description.
	Message string

	// Fingerprint is the dedup key. If empty, it will be computed from
	// Source + Context.
	Fingerprint string

	// Context carries structured fields. Encoded to JSON for storage.
	Context map[string]any
}

// Emit records an alert, deduping against an existing row with the same
// fingerprint. On dedup, count is incremented and last_seen_at + context_json
// are refreshed. Returns the row id of the alert (newly created or existing).
//
// Emit is best-effort: any error is logged and returned, but callers should
// generally treat alert failures as non-fatal observability misses.
func Emit(ctx context.Context, db harmonyquery.DBInterface, args EmitArgs) (int64, error) {
	if args.Source == "" {
		return 0, fmt.Errorf("alerts.Emit: Source is required")
	}
	if args.Message == "" {
		return 0, fmt.Errorf("alerts.Emit: Message is required")
	}
	sev := args.Severity
	if sev == "" {
		sev = SeverityWarning
	}

	fp := args.Fingerprint
	if fp == "" {
		fp = Fingerprint(args.Source, args.Context)
	}

	ctxJSON := "{}"
	if len(args.Context) > 0 {
		b, err := json.Marshal(args.Context)
		if err != nil {
			log.Warnw("alerts.Emit: failed to marshal context, falling back to {}",
				"source", args.Source, "err", err)
		} else {
			ctxJSON = string(b)
		}
	}

	nowMs := time.Now().UnixMilli()

	// Single statement using SQLite UPSERT semantics. On conflict on the
	// fingerprint UNIQUE constraint, increment count + refresh last_seen_at +
	// refresh context_json (latest wins). severity / source / message stay as
	// originally inserted (intentional: avoids alert-text churn on dedup).
	const stmt = `
		INSERT INTO curio_alerts
		    (fingerprint, severity, source, message, context_json,
		     first_seen_at, last_seen_at, count, acked)
		VALUES ($1, $2, $3, $4, $5, $6, $6, 1, 0)
		ON CONFLICT(fingerprint) DO UPDATE SET
		    last_seen_at = excluded.last_seen_at,
		    context_json = excluded.context_json,
		    count        = curio_alerts.count + 1,
		    acked        = 0,
		    acked_at     = NULL
	`
	if _, err := db.ExecI(ctx, stmt, fp, string(sev), args.Source, args.Message, ctxJSON, nowMs); err != nil {
		log.Warnw("alerts.Emit: failed to insert/update alert row",
			"source", args.Source, "fingerprint", fp, "err", err)
		return 0, err
	}

	// SQLite-portable lookup of the row id by fingerprint. We don't rely on
	// LAST_INSERT_ROWID() because UPSERT returns it only on insert; on dedup
	// we need the existing row's id.
	var id int64
	if err := db.QueryRowI(ctx, `SELECT id FROM curio_alerts WHERE fingerprint = $1`, fp).Scan(&id); err != nil {
		log.Warnw("alerts.Emit: failed to read back row id",
			"source", args.Source, "fingerprint", fp, "err", err)
		return 0, err
	}
	return id, nil
}

// Recent returns the most recent alerts ordered by last_seen_at DESC. If
// limit <= 0, defaults to 100. If onlyUnacked is true, only acked=0 rows are
// returned.
func Recent(ctx context.Context, db harmonyquery.DBInterface, limit int, onlyUnacked bool) ([]Alert, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `
		SELECT id, fingerprint, severity, source, message, context_json,
		       first_seen_at, last_seen_at, count, acked, acked_at
		FROM curio_alerts
	`
	args := []any{}
	if onlyUnacked {
		q += ` WHERE acked = 0`
	}
	q += ` ORDER BY last_seen_at DESC LIMIT $1`
	args = append(args, limit)

	var rows []Alert
	if err := db.SelectI(ctx, &rows, harmonyquery.RawString(q), args...); err != nil {
		return nil, fmt.Errorf("alerts.Recent: query failed: %w", err)
	}
	for i := range rows {
		_ = rows[i].DecodeContext()
	}
	return rows, nil
}

// Ack marks an alert as acknowledged. No-op if already acked or if the id does
// not exist. Returns the number of rows changed.
func Ack(ctx context.Context, db harmonyquery.DBInterface, id int64) (int, error) {
	res, err := db.ExecI(ctx, `
		UPDATE curio_alerts
		SET acked = 1, acked_at = $1
		WHERE id = $2 AND acked = 0
	`, time.Now().UnixMilli(), id)
	if err != nil {
		return 0, fmt.Errorf("alerts.Ack: %w", err)
	}
	return res, nil
}

// Counts returns total / unacked / by-severity counts for dashboard use.
type Counts struct {
	Total    int64            `json:"total"`
	Unacked  int64            `json:"unacked"`
	BySeverity map[string]int64 `json:"by_severity"`
}

// CountsOf returns alert counts (total, unacked, by-severity). Suitable for a
// dashboard header strip.
func CountsOf(ctx context.Context, db harmonyquery.DBInterface) (Counts, error) {
	var c Counts
	c.BySeverity = make(map[string]int64)

	if err := db.QueryRowI(ctx, `SELECT COUNT(*) FROM curio_alerts`).Scan(&c.Total); err != nil {
		return c, err
	}
	if err := db.QueryRowI(ctx, `SELECT COUNT(*) FROM curio_alerts WHERE acked = 0`).Scan(&c.Unacked); err != nil {
		return c, err
	}

	var sevRows []struct {
		Severity string `db:"severity"`
		Count    int64  `db:"c"`
	}
	if err := db.SelectI(ctx, &sevRows, `
		SELECT severity, COUNT(*) AS c
		FROM curio_alerts
		WHERE acked = 0
		GROUP BY severity
	`); err != nil {
		return c, err
	}
	for _, r := range sevRows {
		c.BySeverity[r.Severity] = r.Count
	}
	return c, nil
}
