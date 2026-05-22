package harmonysqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// listMigrationFiles returns the sorted list of migration filenames embedded
// in the binary. Filenames are date-prefixed (YYYYMMDD-...) so lexical sort
// matches application order.
func listMigrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// ApplyMigrations runs every embedded SQL migration in order, idempotently.
// Each migration file is executed as a single multi-statement script. A
// "harmony_schema_migrations" bookkeeping table is created on first run and
// tracks which filenames have been applied successfully; subsequent calls
// skip already-applied files.
//
// The runner intentionally executes each file as one Exec() call so
// `CREATE TABLE IF NOT EXISTS`, indexes, and SQLite-trigger bodies inside
// the file all land atomically (modernc.org/sqlite supports
// multi-statement Exec).
func (d *DB) ApplyMigrations(ctx context.Context) error {
	if _, err := d.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS harmony_schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create harmony_schema_migrations: %w", err)
	}

	files, err := listMigrationFiles()
	if err != nil {
		return err
	}

	applied := map[string]struct{}{}
	rows, err := d.sql.QueryContext(ctx, `SELECT name FROM harmony_schema_migrations`)
	if err != nil {
		return fmt.Errorf("query harmony_schema_migrations: %w", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		applied[n] = struct{}{}
	}
	rows.Close()

	for _, name := range files {
		if _, ok := applied[name]; ok {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := d.sql.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := d.sql.ExecContext(ctx,
			`INSERT INTO harmony_schema_migrations (name) VALUES (?)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// MigrationFiles returns the list of embedded migration filenames (sorted).
// Exported for tests.
func MigrationFiles() ([]string, error) { return listMigrationFiles() }
