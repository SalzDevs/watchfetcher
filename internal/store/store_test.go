package store

import (
	"path/filepath"
	"testing"
)

func TestMigrateAndSourceConstraint(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate idempotent: %v", err)
	}

	// seeded marketplaces: unverified + disabled
	enabled, err := SourceEnabled(db, "chrono24")
	if err != nil || enabled {
		t.Fatalf("chrono24 must be disabled (enabled=%v err=%v)", enabled, err)
	}
	// unknown source: disabled, always
	enabled, _ = SourceEnabled(db, "nonexistent")
	if enabled {
		t.Fatal("unknown source must be disabled")
	}

	// G6: enabling without rights evidence must fail at the DB level
	if _, err := db.Exec(`UPDATE sources SET enabled = 1 WHERE id = 'chrono24'`); err == nil {
		t.Fatal("enabling unverified source must violate CHECK constraint")
	}

	// G6 happy path: with rights evidence, enabling works
	if _, err := db.Exec(`UPDATE sources SET enabled = 1, access_status='approved',
		rights_basis='official_api_tos:test', rights_reviewed_at=strftime('%s','now'), reviewer='test'
		WHERE id = 'chrono24'`); err != nil {
		t.Fatalf("enabling approved source must work: %v", err)
	}
	if enabled, _ := SourceEnabled(db, "chrono24"); !enabled {
		t.Fatal("approved source must be enabled")
	}
}
