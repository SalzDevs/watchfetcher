// Command api: HTTP service for WatchFairValue verdicts.
// Reads pricing.sqlite (WAL, read-only) and serves exact-cell verdicts.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/engine"
	"watchfetcher/internal/store"
)

func main() {
	dbPath := flag.String("db", "", "path to pricing.sqlite (env DB_PATH)")
	addr := flag.String("addr", "", "listen addr, e.g. :8080 (env PORT)")
	flag.Parse()

	if *dbPath == "" {
		*dbPath = os.Getenv("DB_PATH")
		if *dbPath == "" {
			*dbPath = "data/pricing.sqlite"
		}
	}
	if *addr == "" {
		// Fly sets PORT, local default 8080
		if p := os.Getenv("PORT"); p != "" {
			*addr = ":" + strings.TrimPrefix(p, ":")
		} else {
			*addr = ":8080"
		}
	}

	db, err := openDB(*dbPath)
	if err != nil {
		log.Fatalf("open db %s: %v", *dbPath, err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	srv := &server{db: db, started: time.Now()}

	// Health
	mux.HandleFunc("GET /health", srv.handleHealth)
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	// Verdict
	mux.HandleFunc("GET /api/verdict", srv.handleVerdict)
	mux.HandleFunc("GET /api/verdicts", srv.handleVerdicts)
	mux.HandleFunc("GET /api/references", srv.handleReferences)
	mux.HandleFunc("GET /api/collections", srv.handleCollections)
	mux.HandleFunc("GET /api/reference", srv.handleReferenceLookup)
	mux.HandleFunc("GET /api/comps", srv.handleComps)
	mux.HandleFunc("GET /api/history", srv.handleHistory)

	// Meta
	mux.HandleFunc("GET /api/meta/brands", srv.handleMetaBrands)
	mux.HandleFunc("GET /api/meta/models", srv.handleMetaModels)
	mux.HandleFunc("GET /api/meta/dials", srv.handleMetaDials)
	mux.HandleFunc("GET /api/meta/materials", srv.handleMetaMaterials)
	mux.HandleFunc("GET /api/meta/scopes", srv.handleMetaScopes)
	mux.HandleFunc("GET /api/meta/stats", srv.handleMetaStats)

	// CORS + logging wrapper
	handler := withCORS(withLogging(mux))

	log.Printf("api listening on %s db=%s", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

func openDB(path string) (*sql.DB, error) {
	// Try read-only first (allows concurrent cron writer). If file missing, create empty schema so API still serves.
	db, err := store.OpenReadOnly(path)
	if err == nil {
		return db, nil
	}
	// If RO fails because file doesn't exist, create via regular Open (creates schema) then reopen RO.
	if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "unable to open") {
		log.Printf("db not found at %s, creating empty: %v", path, err)
		// Ensure dir exists
		if dir := dirOf(path); dir != "" {
			_ = os.MkdirAll(dir, 0755)
		}
		tmp, err2 := store.Open(path)
		if err2 != nil {
			return nil, fmt.Errorf("create db: %w", err2)
		}
		tmp.Close()
		return store.OpenReadOnly(path)
	}
	return nil, err
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

type server struct {
	db      *sql.DB
	started time.Time
}

// verdictResponse is API shape without confidence (internal).
type verdictResponse struct {
	CellKey     string           `json:"cell_key"`
	Brand       string           `json:"brand"`
	Model       string           `json:"model"`
	Dial        string           `json:"dial"`
	Material    string           `json:"material"`
	Scope       string           `json:"scope"`
	FairLow     float64          `json:"fair_low"`
	FairHigh    float64          `json:"fair_high"`
	Median      float64          `json:"median"`
	P25         float64          `json:"p25"`
	P75         float64          `json:"p75"`
	Count       int              `json:"count"`
	ComputedAt  string           `json:"computed_at"`
	HeroImageURL string          `json:"hero_image_url,omitempty"`
	Receipts    []engine.Receipt `json:"receipts"`
}

func toResponse(v *engine.Verdict) verdictResponse {
	hero := ""
	for _, r := range v.Receipts {
		if r.ImageURL != "" {
			hero = r.ImageURL
			break
		}
	}
	return verdictResponse{
		CellKey:      v.CellKey,
		Brand:        canonicalBrand(v.Brand),
		Model:        canonicalModel(v.Model),
		Dial:         v.Dial,
		Material:     v.Material,
		Scope:        v.Scope,
		FairLow:      v.FairLow,
		FairHigh:     v.FairHigh,
		Median:       v.Median,
		P25:          v.P25,
		P75:          v.P75,
		Count:        v.Count,
		ComputedAt:   v.ComputedAt.UTC().Format(time.RFC3339),
		HeroImageURL: hero,
		Receipts:     v.Receipts,
	}
}

func canonicalBrand(b string) string {
	for _, br := range attrs.Brands() {
		if strings.EqualFold(br, b) {
			return br
		}
	}
	// Fallback: title-case
	if b == "" {
		return ""
	}
	return b
}
func canonicalModel(m string) string {
	for _, mm := range attrs.AllModels() {
		if strings.EqualFold(mm, m) {
			return mm
		}
	}
	return m
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	verdicts, observations, computedAt, _ := store.Stats(s.db)
	var comp string
	if computedAt != nil {
		comp = computedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"uptime_s":     int(time.Since(s.started).Seconds()),
		"verdicts":     verdicts,
		"observations": observations,
		"computed_at":  comp,
	})
}

func (s *server) handleVerdict(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cellKey := strings.TrimSpace(q.Get("cell_key"))
	if cellKey == "" {
		// Reference-first path: the factory SKU derives brand/model/dial/material.
		// Unknown ref → 404, never a silent fallback to free-text parsing.
		if ref := strings.TrimSpace(q.Get("ref")); ref != "" {
			s.handleVerdictByRef(w, r, ref)
			return
		}
		brand := strings.TrimSpace(q.Get("brand"))
		model := strings.TrimSpace(q.Get("model"))
		if brand == "" || model == "" {
			writeErr(w, http.StatusBadRequest, "missing_brand_or_model", "brand and model are required (or provide cell_key)")
			return
		}
		// Validate taxonomy
		if !isValidBrand(brand) {
			writeErr(w, http.StatusBadRequest, "unknown_brand", fmt.Sprintf("unknown brand %q", brand))
			return
		}
		if !isValidModel(model) {
			// Allow any model but warn; still try CellKey. If not found, 404.
		}
		dial := strings.TrimSpace(q.Get("dial"))
		material := strings.TrimSpace(q.Get("material"))
		scope := strings.TrimSpace(q.Get("scope"))
		cellKey = engine.CellKey(brand, model, dial, material, scope)
	}
	v, err := store.GetVerdict(s.db, cellKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if v == nil {
		writeErr(w, http.StatusNotFound, "no_verdict", "Need at least 4 sales of that exact configuration; try removing dial/material/scope (unknown matches unknown)")
		return
	}
	// Cache 1h, immutable per nightly computed_at
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf("W/\"%d-%s\"", v.ComputedAt.Unix(), v.CellKey))
	writeJSON(w, http.StatusOK, toResponse(v))
}

// handleVerdictByRef resolves ?ref=SKU: derives the exact 5-tuple from the
// catalogued reference (scope still user-selectable) and delegates to GetVerdict.
func (s *server) handleVerdictByRef(w http.ResponseWriter, r *http.Request, ref string) {
	entry, ok := attrs.LookupRef(ref)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown_ref", fmt.Sprintf("reference %q is not in the catalogued taxonomy", ref))
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	cellKey := engine.CellKey(entry.Brand, entry.Model, entry.Dial, entry.Material, scope)
	v, err := store.GetVerdict(s.db, cellKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if v == nil {
		writeErr(w, http.StatusNotFound, "no_verdict", fmt.Sprintf("Need at least 4 sales of %s (%s, %s); try removing scope", entry.Ref, entry.Dial, entry.Material))
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", fmt.Sprintf("W/\"%d-%s\"", v.ComputedAt.Unix(), v.CellKey))
	writeJSON(w, http.StatusOK, toResponse(v))
}

// handleReferences serves the reference picker for one brand+model family:
// every catalogued SKU with its derived dial/material and the verdict at the
// scope-unknown cell (count/median/hero). count < 4 → no verdict, greyed in UI.
func (s *server) handleReferences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	brand := strings.TrimSpace(q.Get("brand"))
	model := strings.TrimSpace(q.Get("model"))
	if brand == "" || model == "" {
		writeErr(w, http.StatusBadRequest, "missing_brand_or_model", "brand and model are required")
		return
	}
	if !isValidBrand(brand) {
		writeErr(w, http.StatusBadRequest, "unknown_brand", fmt.Sprintf("unknown brand %q", brand))
		return
	}
	rows, err := store.ListReferences(s.db, brand, model)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	type refResp struct {
		Ref          string  `json:"ref"`
		Brand        string  `json:"brand"`
		Model        string  `json:"model"`
		Dial         string  `json:"dial"`
		Material     string  `json:"material"`
		Image        string  `json:"image"`
		Count        int     `json:"count"`
		Median       float64 `json:"median"`
		HeroImageURL string  `json:"hero_image_url,omitempty"`
	}
	out := make([]refResp, 0, len(rows))
	for _, row := range rows {
		out = append(out, refResp{
			Ref:          row.Ref,
			Brand:        canonicalBrand(row.Brand),
			Model:        canonicalModel(row.Model),
			Dial:         row.Dial,
			Material:     row.Material,
			Image:        attrs.CuratedImagePath(brand, model, row.Ref),
			Count:        row.Count,
			Median:       row.Median,
			HeroImageURL: row.HeroImageURL,
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"brand":       canonicalBrand(brand),
		"model":       canonicalModel(model),
		"references":  out,
	})
}

// handleCollections serves the image-first vault picker:
//   - no brand param → maison tiles: [{brand, model_count, ref_count, total_comps, image}]
//   - ?brand=X       → model tiles: [{model, ref_count, total_comps, best_ref, image}]
func (s *server) handleCollections(w http.ResponseWriter, r *http.Request) {
	brand := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brand == "" {
		list, err := store.ListBrandCollections(s.db)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		writeJSON(w, http.StatusOK, map[string]any{"collections": list})
		return
	}
	if !isValidBrand(brand) {
		writeErr(w, http.StatusBadRequest, "unknown_brand", fmt.Sprintf("unknown brand %q", brand))
		return
	}
	models, err := store.ListModelCollections(s.db, brand)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{"brand": canonicalBrand(brand), "models": models})
}

// handleReferenceLookup resolves one catalogued ref to its factory config plus
// the scope-unknown verdict summary. Powers ⌘K ref-paste search.
func (s *server) handleReferenceLookup(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeErr(w, http.StatusBadRequest, "missing_ref", "ref is required")
		return
	}
	entry, ok := attrs.LookupRef(ref)
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown_ref", fmt.Sprintf("reference %q is not in the catalogued taxonomy", ref))
		return
	}
	v, _ := store.GetVerdict(s.db, engine.CellKey(entry.Brand, entry.Model, entry.Dial, entry.Material, ""))
	resp := map[string]any{
		"ref": entry.Ref, "brand": entry.Brand, "model": entry.Model,
		"dial": entry.Dial, "material": entry.Material,
		"image": attrs.CuratedImagePath(entry.Brand, entry.Model, entry.Ref),
		"count": 0, "median": 0.0,
	}
	if v != nil {
		resp["count"] = v.Count
		resp["median"] = v.Median
		resp["cell_key"] = v.CellKey
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, resp)
}

// handleComps serves raw receipts for a catalogued ref when the exact cell has
// no verdict (<4 comps). Evidence, not a fair-value band — doctrine intact.
func (s *server) handleComps(w http.ResponseWriter, r *http.Request) {
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref == "" {
		writeErr(w, http.StatusBadRequest, "missing_ref", "ref is required")
		return
	}
	comps, err := store.GetRefComps(s.db, ref)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if comps == nil {
		writeErr(w, http.StatusNotFound, "unknown_ref", fmt.Sprintf("reference %q is not in the catalogued taxonomy", ref))
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=900")
	writeJSON(w, http.StatusOK, comps)
}

// handleHistory serves the append-only verdict snapshots for one cell —
// by cell_key or by ref (resolves to the scope-unknown cell).
func (s *server) handleHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cellKey := strings.TrimSpace(q.Get("cell_key"))
	if cellKey == "" {
		if ref := strings.TrimSpace(q.Get("ref")); ref != "" {
			entry, ok := attrs.LookupRef(ref)
			if !ok {
				writeErr(w, http.StatusNotFound, "unknown_ref", fmt.Sprintf("reference %q is not in the catalogued taxonomy", ref))
				return
			}
			cellKey = engine.CellKey(entry.Brand, entry.Model, entry.Dial, entry.Material, "")
		} else {
			writeErr(w, http.StatusBadRequest, "missing_cell_key", "cell_key or ref is required")
			return
		}
	}
	history, err := store.GetVerdictHistory(s.db, cellKey)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"cell_key": cellKey,
		"history":  history,
	})
}

func (s *server) handleVerdicts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := map[string]string{}
	for _, k := range []string{"brand", "model", "dial", "material", "scope"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			filters[k] = v
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	sort := strings.TrimSpace(q.Get("sort"))
	rows, total, err := store.ListVerdicts(s.db, filters, limit, offset, sort)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	// Map to API shape without receipts but with hero image
	type rowResp struct {
		CellKey      string `json:"cell_key"`
		Brand        string `json:"brand"`
		Model        string `json:"model"`
		Dial         string `json:"dial"`
		Material     string `json:"material"`
		Scope        string `json:"scope"`
		FairLow      float64 `json:"fair_low"`
		FairHigh     float64 `json:"fair_high"`
		Median       float64 `json:"median"`
		P25          float64 `json:"p25"`
		P75          float64 `json:"p75"`
		Count        int     `json:"count"`
		ComputedAt   string  `json:"computed_at"`
		HeroImageURL string  `json:"hero_image_url,omitempty"`
	}
	out := make([]rowResp, 0, len(rows))
	for _, vr := range rows {
		out = append(out, rowResp{
			CellKey:      vr.CellKey,
			Brand:        canonicalBrand(vr.Brand),
			Model:        canonicalModel(vr.Model),
			Dial:         vr.Dial,
			Material:     vr.Material,
			Scope:        vr.Scope,
			FairLow:      vr.FairLow,
			FairHigh:     vr.FairHigh,
			Median:       vr.Median,
			P25:          vr.P25,
			P75:          vr.P75,
			Count:        vr.Count,
			ComputedAt:   time.Unix(vr.ComputedAt, 0).UTC().Format(time.RFC3339),
			HeroImageURL: vr.HeroImageURL,
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"total":    total,
		"limit":    limitOrDefault(limit),
		"offset":   offset,
		"verdicts": out,
	})
}

func limitOrDefault(l int) int {
	if l <= 0 || l > 100 {
		return 50
	}
	return l
}

func (s *server) handleMetaBrands(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, attrs.Brands())
}

func (s *server) handleMetaModels(w http.ResponseWriter, r *http.Request) {
	brand := strings.TrimSpace(r.URL.Query().Get("brand"))
	if brand == "" {
		writeJSON(w, http.StatusOK, attrs.AllModels())
		return
	}
	ms := attrs.ModelsForBrand(brand)
	if ms == nil {
		writeErr(w, http.StatusBadRequest, "unknown_brand", fmt.Sprintf("unknown brand %q", brand))
		return
	}
	writeJSON(w, http.StatusOK, ms)
}

func (s *server) handleMetaDials(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, attrs.Dials())
}
func (s *server) handleMetaMaterials(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, attrs.Materials())
}
func (s *server) handleMetaScopes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, attrs.Scopes())
}

func (s *server) handleMetaStats(w http.ResponseWriter, r *http.Request) {
	verdicts, observations, computedAt, err := store.Stats(s.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	var comp string
	if computedAt != nil {
		comp = computedAt.UTC().Format(time.RFC3339)
	}
	// Also count cells with data (distinct observation cells)
	var cells int
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT brand || '|' || model || '|' || dial || '|' || material || '|' || scope) FROM observations WHERE brand != '' AND model != ''`).Scan(&cells)
	writeJSON(w, http.StatusOK, map[string]any{
		"verdicts":     verdicts,
		"observations": observations,
		"cells_with_data": cells,
		"computed_at":  comp,
	})
}

// helpers

func isValidBrand(b string) bool {
	for _, br := range attrs.Brands() {
		if strings.EqualFold(br, b) {
			return true
		}
	}
	return false
}
func isValidModel(m string) bool {
	for _, mm := range attrs.AllModels() {
		if strings.EqualFold(mm, m) {
			return true
		}
	}
	// Also allow family-level (submariner etc) as valid for search
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]string{"error": errCode, "message": msg})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %dms", r.Method, r.URL.Path, r.URL.RawQuery, time.Since(start).Milliseconds())
	})
}
