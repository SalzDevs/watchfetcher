// Command engine: precompute fair-value verdicts for every exact cell that
// has enough data. Run after each nightly crawl; the website reads the
// verdicts table directly — instant, deterministic, coherent per snapshot.
package main

import (
	"database/sql"
	"strings"
	"flag"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/engine"
)

type observationRow struct {
	brand      string
	model      string
	dial       string
	material   string
	scope      string
	ref        string
	title      string
	url        string
	source     string
	priceUSD   float64
	observedAt time.Time
}

func main() {
	dbPath := flag.String("db", "data/pricing.sqlite", "path to the observations database")
	flag.Parse()

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fatal("open db:", err)
	}
	db.Exec("PRAGMA busy_timeout = 5000")
	db.Exec("PRAGMA journal_mode = WAL")

	rows, err := db.Query(`
		SELECT COALESCE(brand, ''), COALESCE(model, ''), COALESCE(dial, ''),
		       COALESCE(material, ''), COALESCE(scope, ''), COALESCE(ref, ''),
		       COALESCE(title, ''), COALESCE(source_url, ''),
		       COALESCE(source_type, 'unknown'), price_usd, observed_at
		FROM observations
		WHERE price_usd > 0 AND observed_at IS NOT NULL
	`)
	if err != nil {
		fatal("read observations:", err)
	}

	var observations []engine.Observation
	for rows.Next() {
		var r observationRow
		var ts float64
		if err := rows.Scan(&r.brand, &r.model, &r.dial, &r.material, &r.scope,
			&r.ref, &r.title, &r.url, &r.source, &r.priceUSD, &ts); err != nil {
			fatal("scan:", err)
		}
		// Enrichment: the legacy data has polluted brands and empty models —
		// derive both from the title via the curated taxonomy. Deterministic:
		// same title always yields the same cell.
		brand := attrs.DetectBrand(r.title)
		if brand == "" {
			brand = validBrand(r.brand)
		}
		modelName := attrs.DetectModel(r.title)
		if modelName == "" {
			modelName = attrs.DetectModelFamily(r.title)
		}
		if brand == "" || modelName == "" {
			continue // unverifiable at brand or model level — not evidence
		}
		observations = append(observations, engine.Observation{
			Brand:      brand,
			Model:      modelName,
			Dial:       r.dial,
			Material:   r.material,
			Scope:      r.scope,
			Ref:        r.ref,
			Title:      r.title,
			URL:        r.url,
			Source:     r.source,
			PriceUSD:   r.priceUSD,
			ObservedAt: time.Unix(int64(ts), 0),
		})
	}
	rows.Close()
	fmt.Printf("loaded %d observations\n", len(observations))

	now := time.Now()
	cells := engine.GroupCells(observations)

	verdicts := []*engine.Verdict{}
	for key, cellObs := range cells {
		parts := splitCellKey(key)
		v := engine.ComputeVerdict(parts[0], parts[1], parts[2], parts[3], parts[4], cellObs, now)
		if v != nil {
			verdicts = append(verdicts, v)
		}
	}

	writeVerdicts(db, verdicts, *dbPath)

	// coverage report
	covered := 0
	for _, v := range verdicts {
		covered += v.Count
	}
	fmt.Printf("\n=== VERDICT COVERAGE ===\n")
	fmt.Printf("cells with data:      %d\n", len(cells))
	fmt.Printf("verdicts computed:    %d\n", len(verdicts))
	fmt.Printf("observations covered: %d of %d (%.0f%%)\n",
		covered, len(observations), float64(covered)*100/float64(max(len(observations), 1)))
	conf := map[string]int{}
	for _, v := range verdicts {
		conf[v.Confidence]++
	}
	fmt.Printf("by confidence:        high=%d medium=%d low=%d\n",
		conf["high"], conf["medium"], conf["low"])
}

func writeVerdicts(db *sql.DB, verdicts []*engine.Verdict, dbPath string) {
	db.Exec("DROP TABLE IF EXISTS verdict_receipts")
	db.Exec("DROP TABLE IF EXISTS verdicts")
	db.Exec(`
		CREATE TABLE verdicts (
			cell_key    TEXT PRIMARY KEY,
			brand       TEXT, model TEXT, dial TEXT, material TEXT, scope TEXT,
			fair_low    REAL, fair_high REAL, median REAL,
			p25 REAL, p75 REAL, count INTEGER,
			confidence  TEXT,
			computed_at INTEGER
		)`)
	db.Exec(`
		CREATE TABLE verdict_receipts (
			cell_key TEXT NOT NULL,
			title    TEXT, price REAL, url TEXT, date TEXT, source TEXT, ref TEXT
		)`)

	tx, err := db.Begin()
	if err != nil {
		fatal("begin tx:", err)
	}
	vstmt, err := tx.Prepare(`
		INSERT INTO verdicts
		(cell_key, brand, model, dial, material, scope,
		 fair_low, fair_high, median, p25, p75, count, confidence, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal("prepare verdicts:", err)
	}
	rstmt, err := tx.Prepare(`
		INSERT INTO verdict_receipts (cell_key, title, price, url, date, source, ref)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal("prepare receipts:", err)
	}

	for _, v := range verdicts {
		ts := v.ComputedAt.Unix()
		if _, err := vstmt.Exec(v.CellKey, v.Brand, v.Model, v.Dial, v.Material, v.Scope,
			v.FairLow, v.FairHigh, v.Median, v.P25, v.P75, v.Count, v.Confidence, ts); err != nil {
			fatal("insert verdict:", err)
		}
		for _, r := range v.Receipts {
			if _, err := rstmt.Exec(v.CellKey, r.Title, r.Price, r.URL, r.Date, r.Source, r.Ref); err != nil {
				fatal("insert receipt:", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		fatal("commit:", err)
	}
	fmt.Printf("wrote %d verdicts + receipts to %s\n", len(verdicts), dbPath)
}

func validBrand(brand string) string {
	for _, b := range attrs.Brands() {
		if strings.EqualFold(b, strings.TrimSpace(brand)) {
			return b
		}
	}
	return ""
}

func splitCellKey(key string) [5]string {
	parts := strings.Split(key, "|")
	out := [5]string{}
	for i := 0; i < 5 && i < len(parts); i++ {
		out[i] = parts[i]
	}
	return out
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
