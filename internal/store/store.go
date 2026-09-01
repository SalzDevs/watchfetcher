package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"watchfetcher/internal/model"
)

// FXRates holds currency conversion to USD.
type FXRates struct {
	GBP float64
	EUR float64
	CHF float64
}

// DefaultRates are conservative fixed rates (overridable via env).
// GBP 1.27, EUR 1.08, CHF 1.12 — nightly drift << verdict band width (~15%).
var DefaultRates = FXRates{GBP: 1.27, EUR: 1.08, CHF: 1.12}

func (r FXRates) ToUSD(price float64, currency string) float64 {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "GBP":
		return price * r.GBP
	case "EUR":
		return price * r.EUR
	case "CHF":
		return price * r.CHF
	default:
		return price // USD or unknown — stored as-is; engine assumes USD
	}
}

// Open opens (or creates) the pricing DB with WAL + busy timeout.
func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("journal_mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		return nil, fmt.Errorf("busy_timeout: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		return nil, fmt.Errorf("foreign_keys: %w", err)
	}
	if err := EnsureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// EnsureSchema creates cumulative tables if missing. Observations is
// append-only time series — one row per listing per nightly batch.
func EnsureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS observations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_type TEXT NOT NULL,
			source_url TEXT NOT NULL,
			native_id TEXT NOT NULL,
			title TEXT,
			brand TEXT,
			model TEXT,
			ref TEXT,
			dial TEXT,
			material TEXT,
			scope TEXT,
			price REAL,
			currency TEXT,
			price_usd REAL NOT NULL,
			observed_at INTEGER NOT NULL,
			image_url TEXT,
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
			UNIQUE(source_type, native_id, observed_at)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_cell ON observations(brand, model, dial, material, scope)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_price ON observations(price_usd)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_observed ON observations(observed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_source ON observations(source_type, native_id)`,
		`CREATE TABLE IF NOT EXISTS verdicts (
			cell_key    TEXT PRIMARY KEY,
			brand       TEXT, model TEXT, dial TEXT, material TEXT, scope TEXT,
			fair_low    REAL, fair_high REAL, median REAL,
			p25 REAL, p75 REAL, count INTEGER,
			confidence  TEXT,
			computed_at INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS verdict_receipts (
			cell_key TEXT NOT NULL,
			title    TEXT, price REAL, url TEXT, date TEXT, source TEXT, ref TEXT, image_url TEXT
		)`,
		// Append-only verdict snapshots — one row per cell per engine run. Enables trend charts.
		`CREATE TABLE IF NOT EXISTS verdict_history (
			cell_key    TEXT NOT NULL,
			computed_at INTEGER NOT NULL,
			count       INTEGER,
			median      REAL,
			p25         REAL,
			p75         REAL,
			PRIMARY KEY (cell_key, computed_at)
		)`,
		`CREATE TABLE IF NOT EXISTS crawl_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at INTEGER NOT NULL,
			finished_at INTEGER,
			jobs_total INTEGER,
			jobs_ok INTEGER,
			jobs_err INTEGER,
			status TEXT,
			meta TEXT
		)`,
		// API indexes
		`CREATE INDEX IF NOT EXISTS idx_verdicts_brand_model ON verdicts(brand, model)`,
		`CREATE INDEX IF NOT EXISTS idx_verdicts_computed ON verdicts(computed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_receipts_cell ON verdict_receipts(cell_key)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_ref ON observations(ref)`,
		`CREATE INDEX IF NOT EXISTS idx_observations_title ON observations(title)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("ensure schema: %w (%s)", err, s[:40])
		}
	}
	// Migrations for existing DBs (Fly volume already has 5220 rows without image_url)
	for _, mig := range []string{
		`ALTER TABLE observations ADD COLUMN image_url TEXT`,
		`ALTER TABLE verdict_receipts ADD COLUMN image_url TEXT`,
	} {
		_, _ = db.Exec(mig) // ignore if column already exists
	}
	return nil
}

// InsertObservations appends listings as observations at batchTime.
// Dedup via UNIQUE(source_type, native_id, observed_at) — re-ingesting the
// same batch is a no-op (INSERT OR IGNORE). Price converted to USD via rates.
func InsertObservations(db *sql.DB, listings []model.Listing, batchTime time.Time, rates FXRates) (int, error) {
	if len(listings) == 0 {
		return 0, nil
	}
	observedAt := batchTime.Unix()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO observations
		(source_type, source_url, native_id, title, brand, model, ref, dial, material, scope, price, currency, price_usd, observed_at, image_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, l := range listings {
		if l.Price <= 0 {
			continue
		}
		currency := strings.ToUpper(strings.TrimSpace(l.Currency))
		if currency == "" {
			currency = "USD"
		}
		priceUSD := rates.ToUSD(l.Price, currency)
		if priceUSD <= 0 {
			continue
		}
		// Normalize empty strings to "" so COALESCE in engine works.
		// Ensure every listing has an image: fallback to empty (frontend shows placeholder), but persist if present.
		img := strings.TrimSpace(l.ImageURL)
		res, err := stmt.Exec(
			l.Source, l.URL, l.NativeID, l.Title, l.Brand, l.Model, l.Ref, l.Dial, l.Material, l.Scope,
			l.Price, currency, priceUSD, observedAt, img,
		)
		if err != nil {
			tx.Rollback()
			return inserted, fmt.Errorf("insert %s:%s: %w", l.Source, l.NativeID, err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return inserted, err
	}
	return inserted, nil
}

// CountObservations returns total rows — useful for health checks.
func CountObservations(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&n)
	return n, err
}
