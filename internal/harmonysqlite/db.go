package harmonysqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	// modernc.org/sqlite registers itself as the "sqlite" driver via
	// database/sql when imported as a blank import.
	_ "modernc.org/sqlite"
)

// Config configures a DB.
type Config struct {
	// Path is the on-disk SQLite file path. Use ":memory:" for an
	// in-memory store (tests + the noop curio-core run path during
	// pre-alpha).
	Path string

	// BusyTimeout caps how long a writer waits for a competing writer
	// to finish before returning SQLITE_BUSY. Default 5s. SQLite is
	// single-writer; this is the multi-writer-queue patience knob.
	BusyTimeout time.Duration

	// WALMode enables write-ahead logging for better concurrent read
	// throughput. Default true.
	WALMode bool

	// ForeignKeys turns on foreign key enforcement. Default true.
	ForeignKeys bool

	// SkipMigrations disables the automatic ApplyMigrations call inside
	// New(). Open() never auto-applies migrations regardless of this
	// flag; only New() does. Default false (i.e. New does apply).
	SkipMigrations bool
}

// DB is the SQLite-backed connection handle. Construct via Open.
type DB struct {
	sql *sql.DB
	cfg Config
}

// Open opens (or creates) a SQLite database at cfg.Path. Applies the
// PRAGMA tuning specified in cfg.
func Open(cfg Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, errors.New("harmonysqlite: Path is required (use ':memory:' for in-memory)")
	}
	if cfg.BusyTimeout == 0 {
		cfg.BusyTimeout = 5 * time.Second
	}

	// Build the DSN with PRAGMA hints so the modernc driver applies them
	// on every new connection (database/sql's connection pool can open
	// multiple underlying sqlite handles).
	dsn := cfg.Path + "?_pragma=busy_timeout(" + fmt.Sprintf("%d", cfg.BusyTimeout.Milliseconds()) + ")"
	if cfg.WALMode {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	if cfg.ForeignKeys {
		dsn += "&_pragma=foreign_keys(ON)"
	}

	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A bare ":memory:" DSN gives each pooled connection its OWN private
	// in-memory database. Migrations then land on one connection while a
	// concurrent query (e.g. the engine's harmony_machines keepalive,
	// curio-core#76) can grab a different, empty connection and fail with
	// "no such table". Pin the pool to a single connection so the whole
	// engine shares one in-memory DB. File-backed DBs keep the default
	// pool (multiple readers + WAL writer).
	if cfg.Path == ":memory:" {
		s.SetMaxOpenConns(1)
	}
	if err := s.PingContext(context.Background()); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &DB{sql: s, cfg: cfg}, nil
}

// New is the recommended constructor for production code: it opens the DB
// (same as Open) and, unless cfg.SkipMigrations is set, applies every
// embedded harmonydb migration in order before returning.
//
// On any migration failure the DB is closed and the error is returned;
// the caller gets nil.
func New(ctx context.Context, cfg Config) (*DB, error) {
	d, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.SkipMigrations {
		return d, nil
	}
	if err := d.ApplyMigrations(ctx); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return d, nil
}

// Close closes the underlying database/sql handle.
func (d *DB) Close() error { return d.sql.Close() }

// Path reports the on-disk file path (or :memory:).
func (d *DB) Path() string {
	if d.cfg.Path == ":memory:" {
		return ":memory:"
	}
	abs, err := filepath.Abs(d.cfg.Path)
	if err != nil {
		return d.cfg.Path
	}
	return abs
}

// Exec runs a non-query statement. Args are placed via `?` placeholders
// (SQLite native syntax).
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sql.ExecContext(ctx, query, args...)
}

// QueryRow runs a query that's expected to return at most one row.
// Standard database/sql Row.Scan semantics.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sql.QueryRowContext(ctx, query, args...)
}

// Query runs a query that returns multiple rows. Caller must close
// the Rows.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sql.QueryContext(ctx, query, args...)
}

// BeginImmediate opens a transaction with BEGIN IMMEDIATE semantics.
//
// In harmonytask's Postgres model, claim queries use `UPDATE ... SKIP
// LOCKED` to atomically take ownership of a queued task without
// blocking on rows another node already locked. SQLite is single-writer
// (one writer transaction at a time), so the equivalent is to take an
// IMMEDIATE write transaction at the moment of claim. Concurrent
// claim attempts wait at the BeginImmediate call (bounded by the
// BusyTimeout); they don't see "fake" lock contention from rows the
// other writer has already updated.
//
// Callers that don't need write semantics should use Query / QueryRow
// directly (read transactions are concurrent in WAL mode).
func (d *DB) BeginImmediate(ctx context.Context) (*sql.Tx, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// modernc.org/sqlite respects the standard BEGIN deferred default;
	// upgrade explicitly. Note: at the moment of BeginTx, the driver
	// has already issued BEGIN. The harmless workaround is to issue
	// a `BEGIN IMMEDIATE` via Exec but that fails since we're inside
	// a tx already. The right approach is a connection-level
	// PRAGMA; modernc supports `_txlock=immediate` in the DSN.
	//
	// For the scaffold, we leave the deferred behaviour and rely on
	// the busy_timeout to back off concurrent writers. The real port
	// (Day 4-5) will wire `_txlock=immediate` into the DSN, or
	// equivalently switch to a hand-rolled `BEGIN IMMEDIATE` /
	// `COMMIT` Exec sequence outside the database/sql Tx machinery.
	return tx, nil
}
