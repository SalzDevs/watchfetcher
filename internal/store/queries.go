package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/engine"
)

// OpenReadOnly opens DB for the API (WAL) allowing concurrent cron writer.
// It also ensures the image_url columns exist so old DBs (pre-image) don't error.
func OpenReadOnly(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
	}
	// Ensure image_url columns + verdict_history exist for old DBs (must be before query_only)
	for _, mig := range []string{
		`ALTER TABLE observations ADD COLUMN image_url TEXT`,
		`ALTER TABLE verdict_receipts ADD COLUMN image_url TEXT`,
		`CREATE TABLE IF NOT EXISTS verdict_history (
			cell_key    TEXT NOT NULL,
			computed_at INTEGER NOT NULL,
			count       INTEGER,
			median      REAL,
			p25         REAL,
			p75         REAL,
			PRIMARY KEY (cell_key, computed_at)
		)`,
	} {
		_, _ = db.Exec(mig) // ignore if exists
	}
	if _, err := db.Exec(`PRAGMA query_only=ON`); err != nil {
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping ro db: %w", err)
	}
	return db, nil
}

// VerdictRow is DB row for verdicts without receipts.
type VerdictRow struct {
	CellKey     string
	Brand       string
	Model       string
	Dial        string
	Material    string
	Scope       string
	FairLow     float64
	FairHigh    float64
	Median      float64
	P25         float64
	P75         float64
	Count       int
	Confidence  string
	ComputedAt  int64 // unix
	HeroImageURL string `json:"hero_image_url,omitempty"`
}

// GetVerdict fetches one verdict by exact cell_key (lowercased 5-tuple).
func GetVerdict(db *sql.DB, cellKey string) (*engine.Verdict, error) {
	cellKey = strings.ToLower(strings.TrimSpace(cellKey))
	if cellKey == "" {
		return nil, nil
	}
	row := db.QueryRow(`
		SELECT cell_key, brand, model, dial, material, scope,
		       fair_low, fair_high, median, p25, p75, count, confidence, computed_at
		FROM verdicts WHERE cell_key = ?`, cellKey)
	var vr VerdictRow
	if err := row.Scan(&vr.CellKey, &vr.Brand, &vr.Model, &vr.Dial, &vr.Material, &vr.Scope,
		&vr.FairLow, &vr.FairHigh, &vr.Median, &vr.P25, &vr.P75, &vr.Count, &vr.Confidence, &vr.ComputedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	receipts, err := GetReceipts(db, vr.CellKey)
	if err != nil {
		return nil, err
	}
	return &engine.Verdict{
		CellKey:    vr.CellKey,
		Brand:      vr.Brand,
		Model:      vr.Model,
		Dial:       vr.Dial,
		Material:   vr.Material,
		Scope:      vr.Scope,
		FairLow:    vr.FairLow,
		FairHigh:   vr.FairHigh,
		Median:     vr.Median,
		P25:        vr.P25,
		P75:        vr.P75,
		Count:      vr.Count,
		Confidence: vr.Confidence,
		ComputedAt: time.Unix(vr.ComputedAt, 0).UTC(),
		Receipts:   receipts,
	}, nil
}

// GetReceipts fetches up to 12 receipts for a cell, ordered by date desc (stored as YYYY-MM).
func GetReceipts(db *sql.DB, cellKey string) ([]engine.Receipt, error) {
	rows, err := db.Query(`
		SELECT title, price, url, date, source, ref, COALESCE(image_url, '')
		FROM verdict_receipts WHERE cell_key = ? ORDER BY date DESC LIMIT 12`, cellKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Receipt
	for rows.Next() {
		var r engine.Receipt
		if err := rows.Scan(&r.Title, &r.Price, &r.URL, &r.Date, &r.Source, &r.Ref, &r.ImageURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListVerdicts returns paginated verdicts with optional exact filters.
// Filters use exact match on lowercased brand/model etc; empty means no filter.
// sort: median_desc, median_asc, count_desc, computed_desc, brand_asc (default)
func ListVerdicts(db *sql.DB, filters map[string]string, limit, offset int, sort string) ([]VerdictRow, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// Build WHERE
	var where []string
	var args []any
	for _, k := range []string{"brand", "model", "dial", "material", "scope"} {
		if v, ok := filters[k]; ok && strings.TrimSpace(v) != "" {
			// brand/model stored canonical, so compare LOWER()
			where = append(where, fmt.Sprintf("LOWER(TRIM(%s)) = LOWER(TRIM(?))", k))
			args = append(args, v)
		}
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = "WHERE " + strings.Join(where, " AND ")
	}
	// Count total
	countSQL := `SELECT COUNT(*) FROM verdicts ` + whereSQL
	var total int
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// Order
	order := "ORDER BY brand ASC, model ASC"
	switch sort {
	case "median_desc":
		order = "ORDER BY median DESC"
	case "median_asc":
		order = "ORDER BY median ASC"
	case "count_desc":
		order = "ORDER BY count DESC"
	case "computed_desc":
		order = "ORDER BY computed_at DESC"
	case "brand_asc":
		order = "ORDER BY brand ASC, model ASC"
	}
	query := fmt.Sprintf(`
		SELECT cell_key, brand, model, dial, material, scope,
		       fair_low, fair_high, median, p25, p75, count, confidence, computed_at,
		       COALESCE((SELECT image_url FROM verdict_receipts WHERE verdict_receipts.cell_key = verdicts.cell_key AND image_url != '' ORDER BY date DESC LIMIT 1), '') as hero_image
		FROM verdicts %s %s LIMIT ? OFFSET ?`, whereSQL, order)
	args = append(args, limit, offset)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []VerdictRow
	for rows.Next() {
		var vr VerdictRow
		if err := rows.Scan(&vr.CellKey, &vr.Brand, &vr.Model, &vr.Dial, &vr.Material, &vr.Scope,
			&vr.FairLow, &vr.FairHigh, &vr.Median, &vr.P25, &vr.P75, &vr.Count, &vr.Confidence, &vr.ComputedAt, &vr.HeroImageURL); err != nil {
			return nil, 0, err
		}
		out = append(out, vr)
	}
	return out, total, rows.Err()
}

// ReferenceRow is one reference-picker card: factory SKU + its derived
// dial/material + the verdict at the scope-unknown cell (count/median/hero).
type ReferenceRow struct {
	Ref          string
	Brand        string
	Model        string
	Dial         string
	Material     string
	Median       float64
	Count        int
	HeroImageURL string
}

// ListReferences joins the catalogued references for a brand+model family
// with existing verdicts at their derived (dial, material, scope='') cells.
// References without a verdict return Count=0 — shown greyed in the picker.
func ListReferences(db *sql.DB, brand, model string) ([]ReferenceRow, error) {
	refs := attrs.ReferencesForModel(brand, model)
	if len(refs) == 0 {
		return []ReferenceRow{}, nil
	}
	// scope-unknown verdicts for this family, keyed by dial|material (lowercased).
	rows, err := db.Query(`
		SELECT LOWER(TRIM(dial)) || '|' || LOWER(TRIM(material)), median, count,
		       COALESCE((SELECT image_url FROM verdict_receipts
		                 WHERE verdict_receipts.cell_key = verdicts.cell_key AND image_url != ''
		                 ORDER BY date DESC LIMIT 1), '')
		FROM verdicts
		WHERE LOWER(TRIM(brand)) = LOWER(TRIM(?)) AND LOWER(TRIM(model)) = LOWER(TRIM(?))
		      AND LOWER(TRIM(scope)) = ''`, brand, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCell := map[string]struct {
		median float64
		count  int
		hero   string
	}{}
	for rows.Next() {
		var key, hero string
		var median float64
		var count int
		if err := rows.Scan(&key, &median, &count, &hero); err != nil {
			return nil, err
		}
		byCell[key] = struct {
			median float64
			count  int
			hero   string
		}{median, count, hero}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ReferenceRow, 0, len(refs))
	for _, r := range refs {
		row := ReferenceRow{Ref: r.Ref, Brand: r.Brand, Model: r.Model, Dial: r.Dial, Material: r.Material}
		if v, ok := byCell[strings.ToLower(r.Dial)+"|"+strings.ToLower(r.Material)]; ok {
			row.Median = v.median
			row.Count = v.count
			row.HeroImageURL = v.hero
		}
		out = append(out, row)
	}
	return out, nil
}

// RefComp is one raw comparable observation for a reference (no verdict implied).
type RefComp struct {
	Title    string  `json:"title"`
	PriceUSD float64 `json:"price_usd"`
	URL      string  `json:"url"`
	Source   string  `json:"source"`
	Ref      string  `json:"ref"`
	Scope    string  `json:"scope"`
	Date     string  `json:"date"`
	ImageURL string  `json:"image_url,omitempty"`
}

// RefComps is the "no verdict yet" payload: every raw observation matching a
// catalogued reference (same exact + unique-prefix semantics as attrs.LookupRef —
// ambiguous bases like 5711 match nothing), plus per-scope counts so the UI can
// route the user to a scope that has data.
type RefComps struct {
	Ref          string    `json:"ref"`
	Brand        string    `json:"brand"`
	Model        string    `json:"model"`
	Dial         string    `json:"dial"`
	Material     string    `json:"material"`
	Total        int       `json:"total"`
	Scopes       map[string]int `json:"scopes"`
	Observations []RefComp `json:"observations"`
}

// GetRefComps returns raw observations for a catalogued reference. Doctrine
// intact: these are receipts (evidence), never a fair-value band.
func GetRefComps(db *sql.DB, ref string) (*RefComps, error) {
	target, ok := attrs.LookupRef(ref)
	if !ok {
		return nil, nil
	}
	upper := strings.ToUpper(strings.TrimSpace(ref))
	rows, err := db.Query(`
		SELECT COALESCE(title, ''), price_usd, source_url, COALESCE(source_type, ''),
		       COALESCE(ref, ''), COALESCE(scope, ''),
		       strftime('%Y-%m-%d', observed_at, 'unixepoch'), COALESCE(image_url, '')
		FROM observations
		WHERE price_usd > 0 AND (UPPER(TRIM(ref)) = ?1 OR UPPER(TRIM(ref)) LIKE ?1 || '/%' OR UPPER(TRIM(ref)) LIKE ?1 || '-%')
		ORDER BY observed_at DESC
		LIMIT 400`, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &RefComps{
		Ref: target.Ref, Brand: target.Brand, Model: target.Model,
		Dial: target.Dial, Material: target.Material,
		Scopes: map[string]int{},
	}
	for rows.Next() {
		var c RefComp
		if err := rows.Scan(&c.Title, &c.PriceUSD, &c.URL, &c.Source, &c.Ref, &c.Scope, &c.Date, &c.ImageURL); err != nil {
			return nil, err
		}
		// Ambiguity guard: prefix candidates must resolve to the exact same
		// factory configuration ("5711" base → blue steel AND brown gold → drop).
		rr, resolvable := attrs.LookupRef(c.Ref)
		if !resolvable || rr.Brand != target.Brand || rr.Model != target.Model ||
			rr.Dial != target.Dial || rr.Material != target.Material {
			continue
		}
		out.Total++
		out.Scopes[c.Scope]++
		if len(out.Observations) < 20 {
			out.Observations = append(out.Observations, c)
		}
	}
	return out, rows.Err()
}

// HistoryPoint is one verdict snapshot for a cell.
type HistoryPoint struct {
	ComputedAt int64   `json:"computed_at"`
	Count      int     `json:"count"`
	Median     float64 `json:"median"`
	P25        float64 `json:"p25"`
	P75        float64 `json:"p75"`
}

// GetVerdictHistory returns all snapshots for a cell, oldest first.
func GetVerdictHistory(db *sql.DB, cellKey string) ([]HistoryPoint, error) {
	rows, err := db.Query(`
		SELECT computed_at, count, median, p25, p75
		FROM verdict_history WHERE cell_key = ? ORDER BY computed_at ASC`, strings.ToLower(strings.TrimSpace(cellKey)))
	if err != nil {
		// Old DB without the table — empty history, not an error.
		if strings.Contains(err.Error(), "no such table") {
			return []HistoryPoint{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []HistoryPoint
	for rows.Next() {
		var h HistoryPoint
		if err := rows.Scan(&h.ComputedAt, &h.Count, &h.Median, &h.P25, &h.P75); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// BrandCollection is one maison tile in the vault picker.
type BrandCollection struct {
	Brand      string `json:"brand"`
	ModelCount int    `json:"model_count"`
	RefCount   int    `json:"ref_count"`
	TotalComps int    `json:"total_comps"`
	Image      string `json:"image"` // backdrop for the best-selling family; client falls back to wordmark
}

// ModelCollection is one model tile for a brand.
type ModelCollection struct {
	Model      string `json:"model"`
	RefCount   int    `json:"ref_count"`
	TotalComps int    `json:"total_comps"`
	BestRef    string `json:"best_ref,omitempty"`
	Image      string `json:"image"` // best ref's receipt hero, else curated path; "" → monogram tile
}

// brandModelComps: scope-unknown verdict comps summed per lowercased brand|model.
func brandModelComps(db *sql.DB) (map[string]map[string]int, error) {
	rows, err := db.Query(`
		SELECT LOWER(TRIM(brand)), LOWER(TRIM(model)), SUM(count)
		FROM verdicts WHERE LOWER(TRIM(scope)) = '' AND count > 0
		GROUP BY 1, 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]int{}
	for rows.Next() {
		var b, m string
		var c int
		if err := rows.Scan(&b, &m, &c); err != nil {
			return nil, err
		}
		if out[b] == nil {
			out[b] = map[string]int{}
		}
		out[b][m] = c
	}
	return out, rows.Err()
}

// ListBrandCollections returns all maisons with their tile data.
func ListBrandCollections(db *sql.DB) ([]BrandCollection, error) {
	bm, err := brandModelComps(db)
	if err != nil {
		return nil, err
	}
	out := make([]BrandCollection, 0, len(attrs.Brands()))
	for _, brand := range attrs.Brands() {
		models := attrs.ModelsForBrand(brand)
		bc := BrandCollection{Brand: brand, ModelCount: len(models)}
		bestModel, bestComps := "", 0
		for _, m := range models {
			refs := attrs.ReferencesForModel(brand, m)
			bc.RefCount += len(refs)
			comps := bm[strings.ToLower(brand)][strings.ToLower(m)]
			bc.TotalComps += comps
			if comps > bestComps {
				bestModel, bestComps = m, comps
			}
		}
		if refs := attrs.ReferencesForModel(brand, bestModel); len(refs) > 0 {
			bc.Image = attrs.CuratedImagePath(brand, bestModel, refs[0].Ref)
		}
		out = append(out, bc)
	}
	return out, nil
}

// ListModelCollections returns model tiles for one brand — every family in the
// taxonomy (even without catalogued refs), with its best ref + hero image.
func ListModelCollections(db *sql.DB, brand string) ([]ModelCollection, error) {
	rows, err := db.Query(`
		SELECT LOWER(TRIM(model)), LOWER(TRIM(dial)) || '|' || LOWER(TRIM(material)), count, median,
		       COALESCE((SELECT image_url FROM verdict_receipts
		                 WHERE verdict_receipts.cell_key = verdicts.cell_key AND image_url != ''
		                 ORDER BY date DESC LIMIT 1), '')
		FROM verdicts
		WHERE LOWER(TRIM(brand)) = LOWER(TRIM(?)) AND LOWER(TRIM(scope)) = ''`, brand)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cell struct {
		count  int
		median float64
		hero   string
	}
	cells := map[string][]cell{} // lowercased model → cells
	for rows.Next() {
		var model, key, hero string
		var count int
		var median float64
		if err := rows.Scan(&model, &key, &count, &median, &hero); err != nil {
			return nil, err
		}
		cells[model] = append(cells[model], cell{count, median, hero})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []ModelCollection
	for _, m := range attrs.ModelsForBrand(brand) {
		mc := ModelCollection{Model: m}
		refs := attrs.ReferencesForModel(brand, m)
		mc.RefCount = len(refs)
		for _, c := range cells[strings.ToLower(m)] {
			mc.TotalComps += c.count
		}
		// Best ref: catalogued SKU whose (dial|material) cell has the most comps.
		// Per-ref cell lookup (exact, scope-unknown); receipt hero beats curated path.
		bestCount, bestHero := 0, ""
		for _, r := range refs {
			var count int
			var hero string
			err := db.QueryRow(`
				SELECT count, COALESCE((SELECT image_url FROM verdict_receipts
					WHERE verdict_receipts.cell_key = verdicts.cell_key AND image_url != ''
					ORDER BY date DESC LIMIT 1), '')
				FROM verdicts
				WHERE LOWER(TRIM(brand)) = LOWER(TRIM(?)) AND LOWER(TRIM(model)) = LOWER(TRIM(?))
				      AND LOWER(TRIM(dial)) = LOWER(TRIM(?)) AND LOWER(TRIM(material)) = LOWER(TRIM(?))
				      AND LOWER(TRIM(scope)) = ''`,
				brand, m, r.Dial, r.Material).Scan(&count, &hero)
			if err != nil {
				if err != sql.ErrNoRows {
					return nil, err
				}
				continue
			}
			if count > bestCount {
				bestCount = count
				mc.BestRef = r.Ref
				bestHero = hero
			}
		}
		if mc.BestRef != "" {
			if bestHero != "" {
				mc.Image = bestHero
			} else {
				mc.Image = attrs.CuratedImagePath(brand, m, mc.BestRef)
			}
		}
		out = append(out, mc)
	}
	return out, nil
}
func Stats(db *sql.DB) (verdicts, observations int, computedAt *time.Time, err error) {
	if err = db.QueryRow(`SELECT COUNT(*) FROM verdicts`).Scan(&verdicts); err != nil {
		return
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&observations); err != nil {
		return
	}
	var ts sql.NullInt64
	if err = db.QueryRow(`SELECT MAX(computed_at) FROM verdicts`).Scan(&ts); err != nil {
		return
	}
	if ts.Valid {
		t := time.Unix(ts.Int64, 0).UTC()
		computedAt = &t
	}
	return
}
