// Package engine wires the harmonytask task-engine surface against
// curio-core's SQLite-backed state store and the trimmed PDP task set.
//
// # Architectural shape
//
// Upstream Curio runs `harmonytask.New(db *harmonydb.DB, impls, ...)`
// where `*harmonydb.DB` is a concrete pgx-backed Postgres pool
// (`*harmonyquery.DB` under the hood). curio-core's storage layer is
// SQLite (`internal/harmonysqlite`), so the upstream engine constructor
// cannot be called directly — the DB type is concrete, not an
// interface.
//
// Day 5 ships the curio-core-side surface around that gap:
//
//   - Opens the SQLite state DB at the configured path.
//   - Builds a TaskRegistry of curio TaskTypeDetails for every PDP v1
//     task that can be constructed under `CGO_ENABLED=0` without
//     deferenceable deps, plus a static descriptor table for PDP v0
//     (whose package still pulls in the lotus/storage/paths + gosigar
//     transitives the Day 6 carve-out will retire).
//   - Records this machine as the lone row of `harmony_machines`
//     (curio-core is single-server; no Peering layer ever runs).
//   - Exposes Start(ctx) / Stop() / Healthy() for the daemon lifecycle.
//
// The actual scheduler goroutine + adder loop is intentionally NOT
// started here: invoking `harmonytask.New` is blocked on the
// fork-side `*harmonydb.DB` → interface refactor (tracked as a TODO
// in `docs/DAY-5-NOTES.md`, §"Fork follow-ups"). Once that lands, the
// Start() body grows the `harmonytask.New(...)` call; everything else
// (registry shape, machine row, lifecycle) stays put.
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/tasks/pdpv0"
	"github.com/filecoin-project/curio/tasks/tasknames"

	"github.com/Reiers/curio-core/internal/harmonysqlite"
)

// Config configures an Engine.
type Config struct {
	// DBPath is the on-disk SQLite path. Empty means use DefaultDBPath().
	DBPath string

	// HostAndPort is the value recorded in harmony_machines.host_and_port.
	// Used as a stable identity for this single-server node. Defaults to
	// the OS hostname plus a curio-core sentinel port if empty.
	HostAndPort string
}

// DefaultDBPath resolves the canonical state.sqlite location:
//   - $XDG_DATA_HOME/curio-core/state.sqlite if XDG_DATA_HOME is set
//   - $HOME/.local/share/curio-core/state.sqlite otherwise (XDG default)
//   - /tmp/curio-core/state.sqlite if home is unreachable
func DefaultDBPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "curio-core", "state.sqlite")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/curio-core/state.sqlite"
	}
	return filepath.Join(home, ".local", "share", "curio-core", "state.sqlite")
}

// Engine owns the SQLite state handle and the curio task-type registry.
// Construct with New; lifecycle is Start → Stop. Healthy() reports
// the running state for liveness probes.
type Engine struct {
	cfg Config
	db  *harmonysqlite.DB

	registry *TaskRegistry

	// te is the harmonytask scheduler engine. Constructed in Start,
	// drained in Stop. nil outside the running window.
	te *harmonytask.TaskEngine

	startedAt atomic.Value // time.Time, optional, zero until Start()
	running   atomic.Bool

	mu      sync.Mutex
	stopErr error
}

// New opens the SQLite state DB (applying migrations) and builds the
// task registry. It does NOT start the scheduler — call Start.
func New(ctx context.Context, cfg Config) (*Engine, error) {
	if cfg.DBPath == "" {
		cfg.DBPath = DefaultDBPath()
	}
	if cfg.HostAndPort == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "curio-core"
		}
		// Sentinel port: curio-core is single-server, the host:port
		// triplet just needs to be stable + unique within this DB.
		cfg.HostAndPort = host + ":0"
	}

	// Ensure parent dir exists for file-backed DBs.
	if cfg.DBPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
			return nil, fmt.Errorf("engine: mkdir state dir: %w", err)
		}
	}

	db, err := harmonysqlite.New(ctx, harmonysqlite.Config{
		Path:        cfg.DBPath,
		WALMode:     true,
		ForeignKeys: true,
	})
	if err != nil {
		return nil, fmt.Errorf("engine: open state db: %w", err)
	}

	reg, err := BuildTaskRegistry()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("engine: build task registry: %w", err)
	}

	return &Engine{
		cfg:      cfg,
		db:       db,
		registry: reg,
	}, nil
}

// DB returns the underlying SQLite handle. Useful for code that wants
// to share a single state DB with the engine (firstrun config, the
// setup webui) without re-opening the file.
func (e *Engine) DB() *harmonysqlite.DB { return e.db }

// Registry returns the immutable task registry built at New() time.
func (e *Engine) Registry() *TaskRegistry { return e.registry }

// Start prepares the engine for task execution.
//
// Day 5 scope: records this machine in harmony_machines and flips the
// running flag. The harmonytask scheduler / adder loop is NOT started
// here; that wire-up unblocks once the fork's harmonydb.DB lands as
// an interface (see DAY-5-NOTES.md §"Fork follow-ups").
//
// extraTasks is an optional list of additional harmonytask.TaskInterface
// instances to register alongside the built-in pdpv0 set. Used by
// cmd/curio-core/main to thread message.SendTaskETH (built from the
// embedded Lantern ethclient) into the scheduler at boot.
//
// Returns an error if Start has already been called.
func (e *Engine) Start(ctx context.Context, extraTasks ...harmonytask.TaskInterface) error {
	if !e.running.CompareAndSwap(false, true) {
		return errors.New("engine: already started")
	}

	machineID, err := e.recordMachineRow(ctx)
	if err != nil {
		e.running.Store(false)
		return fmt.Errorf("engine: record machine row: %w", err)
	}

	// Wire the harmonytask scheduler against our SQLite-backed
	// *harmonysqlite.DB. The DB satisfies harmonyquery.DBInterface via the
	// adapter in internal/harmonysqlite/dbinterface.go, so the upstream
	// scheduler runs unmodified against SQLite.
	//
	// We bypass resources.Register because its upsert SQL uses Postgres
	// CTE + RETURNING + $N placeholders that SQLite can't parse. Instead
	// we record the machine row ourselves with SQLite-friendly SQL (see
	// recordMachineRow) and hand-build a *resources.Reg with the resulting
	// MachineID. Same end-state, just SQL-dialect-aware.
	//
	// Wire the first real pdpv0 task: PDPNotifyTask. Constructor takes
	// (ctx, db) only; no chain-API, no sender, no indexstore deps.
	// The poll goroutine watches pdp_piece_uploads + parked_pieces;
	// when an upload finishes, the task fires and the AddTaskFunc
	// closure drives a real harmonytask transaction.
	//
	// This is the Day 7 milestone: a real upstream PDP task instance
	// running inside curio-core, against SQLite, with the harmonytask
	// scheduler driving it. Heavier pdpv0 tasks (Prove, NextPP, etc.)
	// wire in as their chain-API/sender/indexstore deps come online.
	notify := pdpv0.NewPDPNotifyTask(ctx, e.db)
	impls := []harmonytask.TaskInterface{notify}
	impls = append(impls, extraTasks...)

	reg := &resources.Reg{
		Resources: resources.Resources{
			Cpu:       1,
			Gpu:       0,
			Ram:       1 << 30,
			MachineID: machineID,
		},
	}
	te, err := harmonytask.NewWithReg(e.db, impls, e.cfg.HostAndPort, nil /* peerConnector */, reg)
	if err != nil {
		e.running.Store(false)
		return fmt.Errorf("engine: harmonytask.NewWithReg: %w", err)
	}
	e.te = te

	return nil
}

// recordMachineRow inserts (or updates) the lone harmony_machines row
// representing this curio-core instance. Single-server: there is at
// most one row per HostAndPort.
//
// We deliberately do NOT call resources.Register from the upstream
// fork: that helper requires a Postgres-shaped CTE/RETURNING upsert
// that SQLite can't parse. Instead we record the row ourselves and
// return the resulting machine_id so the caller can hand-build a
// *resources.Reg.
func (e *Engine) recordMachineRow(ctx context.Context) (int, error) {
	// UPDATE-first, INSERT-if-zero. host_and_port isn't unique in
	// upstream's schema, but curio-core is single-server so we keep
	// exactly one row keyed by HostAndPort.
	res, err := e.db.Exec(ctx, `
		UPDATE harmony_machines
		   SET cpu = ?, ram = ?, gpu = ?, last_contact = CURRENT_TIMESTAMP
		 WHERE host_and_port = ?`,
		1, int64(1<<30), 0, e.cfg.HostAndPort)
	if err != nil {
		return 0, fmt.Errorf("update harmony_machines: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		if _, err := e.db.Exec(ctx, `
			INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu, last_contact)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			e.cfg.HostAndPort, 1, int64(1<<30), 0); err != nil {
			return 0, fmt.Errorf("insert harmony_machines: %w", err)
		}
	}

	// Read back the row's ID for the Reg.
	var id int
	if err := e.db.QueryRow(ctx,
		`SELECT id FROM harmony_machines WHERE host_and_port = ? ORDER BY id DESC LIMIT 1`,
		e.cfg.HostAndPort,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("select machine_id: %w", err)
	}
	return id, nil
}

// te holds the harmonytask engine when running. nil before Start /
// after Stop.
// (declared as a field via the existing Engine struct further up.)

// Stop shuts the engine down.
//
// Day 5 scope: marks not-running, closes the SQLite handle. When the
// scheduler wire-up lands (post-fork-interface-refactor) Stop will
// gain a graceful drain of in-flight tasks before closing the DB.
func (e *Engine) Stop() error {
	if !e.running.CompareAndSwap(true, false) {
		return nil // idempotent
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.te != nil {
		e.te.GracefullyTerminate()
		e.te = nil
	}
	if e.db != nil {
		if err := e.db.Close(); err != nil {
			e.stopErr = err
			return fmt.Errorf("engine: close db: %w", err)
		}
		e.db = nil
	}
	return nil
}

// Healthy reports whether the engine is in its post-Start, pre-Stop
// running window. Cheap; callable from any goroutine.
func (e *Engine) Healthy() bool { return e.running.Load() }

// ---------------------------------------------------------------------
// TaskRegistry
// ---------------------------------------------------------------------

// TaskRegistry holds the harmonytask.TaskTypeDetails for every task
// type curio-core schedules. Built once at engine construction; safe
// for concurrent read.
type TaskRegistry struct {
	byName map[string]harmonytask.TaskTypeDetails
}

// Names returns the registered task names in deterministic order
// (alphabetical) for stable test assertions.
func (r *TaskRegistry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	// stable order
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// Get returns the TaskTypeDetails for the given task name, or
// (zero, false) if unregistered.
func (r *TaskRegistry) Get(name string) (harmonytask.TaskTypeDetails, bool) {
	td, ok := r.byName[name]
	return td, ok
}

// Has reports whether name is registered.
func (r *TaskRegistry) Has(name string) bool {
	_, ok := r.byName[name]
	return ok
}

// Len is the count of registered task types.
func (r *TaskRegistry) Len() int { return len(r.byName) }

// BuildTaskRegistry assembles the curio-core task registry. It pulls
// real TaskTypeDetails from the upstream PDP v1 package for the tasks
// whose constructors are safe to call with nil deps (the constructor
// is required by the package surface; nothing about TypeDetails()
// itself dereferences those deps), and uses static descriptors for
// the rest.
//
// PDP v0 task types are entirely static-descriptor: the `tasks/pdpv0`
// package transitively pulls in `lotus/storage/paths` + `gosigar`,
// neither of which compiles under `CGO_ENABLED=0` today (Day 6's
// carve-out fixes that). The static descriptors use `tasknames.PDPv0_*`
// for naming consistency.
//
// Scope (2026-05-23, Andy via Nicklas): pdpv0-only. `tasks/pdp` (v1, the
// mk20-deal-flow PDP) is intentionally NOT registered here. The v1
// package + its `curio/market/mk20` transitive are out of scope for
// curio-core. If v1 is reintroduced later, restore the safeCtors block
// from git history at commit fd85e79.
func BuildTaskRegistry() (*TaskRegistry, error) {
	r := &TaskRegistry{byName: make(map[string]harmonytask.TaskTypeDetails, 32)}

	// --- PDP v0: static descriptors (package not yet importable). ---
	// Day 6 swaps these for live `pdpv0.New*` calls once the lotus
	// storage paths + gosigar carve-out lands.
	staticV0 := []harmonytask.TaskTypeDetails{
		{
			Name:        tasknames.PDPv0_Prove,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 256 << 20},
			MaxFailures: 5,
		},
		{
			Name:        tasknames.PDPv0_PullPiece,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_SaveCache,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_InitPP,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_ProvPeriod,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_Notify,
			Cost:        resources.Resources{Cpu: 1, Ram: 128 << 20},
			MaxFailures: 14,
		},
		{
			Name:        tasknames.PDPv0_DelDataSet,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_TermFWSS,
			Max:         taskhelp.Max(50),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
		{
			Name:        tasknames.PDPv0_ChainSync,
			Max:         taskhelp.Max(1),
			Cost:        resources.Resources{Cpu: 1, Ram: 64 << 20},
			MaxFailures: 3,
		},
	}
	for _, td := range staticV0 {
		if err := r.register(td); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func (r *TaskRegistry) register(td harmonytask.TaskTypeDetails) error {
	if td.Name == "" {
		return errors.New("task registry: empty Name")
	}
	if _, dup := r.byName[td.Name]; dup {
		return fmt.Errorf("task registry: duplicate name %q", td.Name)
	}
	r.byName[td.Name] = td
	return nil
}
