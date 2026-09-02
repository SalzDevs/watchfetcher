// Package store: SQLite access — schema migrations (embedded, numbered, plain
// SQL) and typed query helpers. No ORM. SQL is the source of truth; constraints
// enforce policy (PLAN.md §13: compliance is structural).
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // pure Go — no CGO, single-binary deploys
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (or creates) the ledger database with WAL + busy timeout.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	for _, p := range []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON"} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	return db, nil
}

// Migrate applies embedded migrations in filename order, tracking progress in
// schema_info. Idempotent: already-applied files are skipped by name.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now')))`); err != nil {
		return fmt.Errorf("migration table: %w", err)
	}
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // numbered prefix = order
	for _, name := range names {
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if applied > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

// SourceEnabled — the ingest path's only on/off query (G6).
// The DB decides, not config files, not code review.
func SourceEnabled(db *sql.DB, id string) (bool, error) {
	var enabled int
	err := db.QueryRow(`SELECT enabled FROM sources WHERE id = ?`, id).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, nil // unknown source = disabled, always
	}
	if err != nil {
		return false, err
	}
	return enabled == 1, nil
}
