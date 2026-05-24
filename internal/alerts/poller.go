package alerts

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/curiostorage/harmonyquery"
)

// Poller observes harmony_task_history for task failures and emits alerts.
//
// V0 design: pull recently-failed task rows from harmony_task_history, dedupe
// against the last observed cursor, and translate each into an alert with
// severity = error and source = "task/<task_name>". Context carries the
// task id, result, work_start, and a truncated err string.
//
// This is deliberately decoupled from the fork's task implementations so we
// can ship alerting without modifying every task. Later we'll layer in
// finer-grained alerts (e.g. consecutive_prove_failures crossing a threshold,
// low-balance warnings, lifecycle sweeper interventions).
type Poller struct {
	db       harmonyquery.DBInterface
	interval time.Duration

	cursorMu sync.Mutex
	cursor   int64 // last seen harmony_task_history.id; rows with id > cursor are new
}

// NewPoller constructs a Poller. Default interval is 30s.
func NewPoller(db harmonyquery.DBInterface, interval time.Duration) *Poller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Poller{db: db, interval: interval}
}

// Run blocks until ctx is cancelled, polling at the configured interval.
//
// On first run, the cursor is initialized to the current max id so we don't
// re-alert on historical failures. Operators who want to backfill should
// vacuum the alerts table and restart with the poller's cursor reset.
func (p *Poller) Run(ctx context.Context) error {
	if err := p.initCursor(ctx); err != nil {
		return err
	}
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := p.tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warnw("alerts.Poller.tick failed", "err", err)
			}
		}
	}
}

func (p *Poller) initCursor(ctx context.Context) error {
	var maxID *int64
	if err := p.db.QueryRowI(ctx, `SELECT MAX(id) FROM harmony_task_history`).Scan(&maxID); err != nil {
		return err
	}
	p.cursorMu.Lock()
	defer p.cursorMu.Unlock()
	if maxID != nil {
		p.cursor = *maxID
	}
	log.Infow("alerts.Poller initialized", "starting_cursor", p.cursor)
	return nil
}

type taskHistoryRow struct {
	ID        int64  `db:"id"`
	TaskID    int64  `db:"task_id"`
	Name      string `db:"name"`
	Result    int    `db:"result"`     // 0 = failed, 1 = succeeded (in our schema)
	WorkStart string `db:"work_start"` // text-shaped timestamp
	Err       string `db:"err"`
}

func (p *Poller) tick(ctx context.Context) error {
	p.cursorMu.Lock()
	cursor := p.cursor
	p.cursorMu.Unlock()

	// Pull new failed rows since cursor. Cap at 100 per tick to keep the
	// per-tick work bounded; if more come in, we catch up on the next tick.
	var rows []taskHistoryRow
	err := p.db.SelectI(ctx, &rows, `
		SELECT id, task_id, name, result, work_start, err
		FROM harmony_task_history
		WHERE id > $1 AND result = 0
		ORDER BY id ASC
		LIMIT 100
	`, cursor)
	if err != nil {
		return err
	}

	// Also pull the matching newest id to advance the cursor even if there were
	// no failures (so we don't keep re-scanning the same range).
	var newCursor *int64
	if err := p.db.QueryRowI(ctx, `SELECT MAX(id) FROM harmony_task_history WHERE id > $1`, cursor).Scan(&newCursor); err != nil {
		return err
	}

	for _, row := range rows {
		// Heuristic: alert as warning for the first failure of a task,
		// promote to error once we see >=3 consecutive failures for the
		// same task_id. The Emit dedup-by-fingerprint path handles the
		// promotion automatically (same fingerprint = count++; we choose
		// severity at emit-time based on the count we've seen so far).
		sev := SeverityWarning

		// Count prior failures for the same task_id in history. Bounded scan.
		var priorFailures int64
		if err := p.db.QueryRowI(ctx, `
			SELECT COUNT(*) FROM harmony_task_history
			WHERE task_id = $1 AND result = 0 AND id <= $2
		`, row.TaskID, row.ID).Scan(&priorFailures); err == nil {
			if priorFailures >= 3 {
				sev = SeverityError
			}
			if priorFailures >= 5 {
				sev = SeverityCritical
			}
		}

		// Truncate err to keep alert messages readable.
		errMsg := row.Err
		if len(errMsg) > 200 {
			errMsg = errMsg[:200] + "..."
		}
		// Keep multi-line errors on one line.
		errMsg = strings.ReplaceAll(errMsg, "\n", " ")

		// Source = task/<name>; fingerprint = task/<name>/<task_id> so each
		// retry of the same task fingerprints together, but different task ids
		// (= different units of work) get distinct alerts.
		source := "task/" + row.Name
		fp := Fingerprint(source, map[string]any{
			"task_id": row.TaskID,
		})

		_, emitErr := Emit(ctx, p.db, EmitArgs{
			Severity:    sev,
			Source:      source,
			Fingerprint: fp,
			Message:     row.Name + " failed (task_id=" + strconv.FormatInt(row.TaskID, 10) + ")",
			Context: map[string]any{
				"task_id":         row.TaskID,
				"name":            row.Name,
				"history_id":      row.ID,
				"work_start":      row.WorkStart,
				"err":             errMsg,
				"prior_failures":  priorFailures,
				"history_result":  row.Result,
			},
		})
		if emitErr != nil {
			log.Warnw("alerts.Poller: Emit failed", "task_id", row.TaskID, "name", row.Name, "err", emitErr)
		}
	}

	if newCursor != nil {
		p.cursorMu.Lock()
		p.cursor = *newCursor
		p.cursorMu.Unlock()
	}
	return nil
}


