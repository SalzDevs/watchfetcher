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
	"watchfetcher/internal/store"
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
	imageURL   string
	priceUSD   float64
	observedAt time.Time
}

func main() {
	dbPath := flag.String("db", "data/pricing.sqlite", "path to the observations database")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open db:", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT COALESCE(brand, ''), COALESCE(model, ''), COALESCE(dial, ''),
		       COALESCE(material, ''), COALESCE(scope, ''), COALESCE(ref, ''),
		       COALESCE(title, ''), COALESCE(source_url, ''),
		       COALESCE(source_type, 'unknown'), COALESCE(image_url, ''), price_usd, observed_at
		FROM observations
		WHERE price_usd > 0 AND observed_at IS NOT NULL
	`)
	if err != nil {
		fatal("read observations:", err)
	}

	var observations []engine.Observation
	refHits, dialFixed, materialFixed, refModelMismatch := 0, 0, 0, 0
	for rows.Next() {
		var r observationRow
		var ts float64
		if err := rows.Scan(&r.brand, &r.model, &r.dial, &r.material, &r.scope,
			&r.ref, &r.title, &r.url, &r.source, &r.imageURL, &r.priceUSD, &ts); err != nil {
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
		dial, material := r.dial, r.material
		// Reference-derived attributes: the factory SKU is ground truth for
		// dial and material. Free-text detection (DetectDial/DetectMaterial)
		// contaminated cells across references (batman vs pepsi share a model
		// family); a known ref pins both and fills the empty ones (watchfinder).
		if r.ref != "" {
			if entry, ok := attrs.LookupRef(r.ref); ok {
				refHits++
				if entry.Brand == brand && entry.Model == modelName {
					if entry.Dial != "" && dial != entry.Dial {
						dialFixed++
						dial = entry.Dial
					}
					if entry.Material != "" && material != entry.Material {
						materialFixed++
						material = entry.Material
					}
				} else {
					refModelMismatch++
				}
			}
		}
		observations = append(observations, engine.Observation{
			Brand:      brand,
			Model:      modelName,
			Dial:       dial,
			Material:   material,
			Scope:      r.scope,
			Ref:        r.ref,
			Title:      r.title,
			URL:        r.url,
			Source:     r.source,
			ImageURL:   r.imageURL,
			PriceUSD:   r.priceUSD,
			ObservedAt: time.Unix(int64(ts), 0),
		})
	}
	rows.Close()
	fmt.Printf("loaded %d observations\n", len(observations))
	fmt.Printf("ref taxonomy: %d refs catalogued; hits=%d dial_fixed=%d material_fixed=%d model_mismatch=%d\n",
		attrs.ReferenceCount(), refHits, dialFixed, materialFixed, refModelMismatch)

	now := time.Now()
	cells := engine.GroupCells(observations)

	verdicts := []*engine.Verdict{}
	for _, cellObs := range cells {
		// Use canonical brand/model from first observation (all share same exact cell).
		// This preserves canonical casing (e.g. "Rolex" not "rolex") vs lowercased splitCellKey.
		if len(cellObs) == 0 {
			continue
		}
		brand := cellObs[0].Brand
		model := cellObs[0].Model
		dial := cellObs[0].Dial
		material := cellObs[0].Material
		scope := cellObs[0].Scope
		v := engine.ComputeVerdict(brand, model, dial, material, scope, cellObs, now)
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
	// Idempotent / cumulative: if we computed zero verdicts but previous
	// verdicts exist (e.g. fetch failed, no new observations), keep old.
	if len(verdicts) == 0 {
		var existing int
		if err := db.QueryRow(`SELECT COUNT(*) FROM verdicts`).Scan(&existing); err == nil && existing > 0 {
			fmt.Printf("no verdicts computed — preserving %d previous verdicts (no new comparable data)\n", existing)
			return
		}
	}
	// Atomic replace: delete + insert in one transaction so a crash never
	// leaves verdicts empty when we had good data.


	tx, err := db.Begin()
	if err != nil {
		fatal("begin tx:", err)
	}
	if _, err := tx.Exec(`DELETE FROM verdict_receipts`); err != nil {
		fatal("clear receipts:", err)
	}
	if _, err := tx.Exec(`DELETE FROM verdicts`); err != nil {
		fatal("clear verdicts:", err)
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
		INSERT INTO verdict_receipts (cell_key, title, price, url, date, source, ref, image_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal("prepare receipts:", err)
	}
	hstmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO verdict_history (cell_key, computed_at, count, median, p25, p75)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fatal("prepare history:", err)
	}

	for _, v := range verdicts {
		ts := v.ComputedAt.Unix()
		if _, err := vstmt.Exec(v.CellKey, v.Brand, v.Model, v.Dial, v.Material, v.Scope,
			v.FairLow, v.FairHigh, v.Median, v.P25, v.P75, v.Count, v.Confidence, ts); err != nil {
			fatal("insert verdict:", err)
		}
		for _, r := range v.Receipts {
			if _, err := rstmt.Exec(v.CellKey, r.Title, r.Price, r.URL, r.Date, r.Source, r.Ref, r.ImageURL); err != nil {
				fatal("insert receipt:", err)
			}
		}
		// Append-only trend snapshot — one per run, deduped on (cell_key, computed_at).
		if _, err := hstmt.Exec(v.CellKey, ts, v.Count, v.Median, v.P25, v.P75); err != nil {
			fatal("insert history:", err)
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
