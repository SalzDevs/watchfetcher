// Package httpx: server-rendered HTTP surface. html/template + htmx — no SPA,
// every evidence page readable without JS (trust product, SEO-native).
package httpx

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var tpl = template.Must(template.New("").Funcs(template.FuncMap{
	"json": func(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) },
}).ParseFS(templatesFS, "templates/*.html"))

// Server holds read-model dependencies. Routes serve precomputed data only —
// no live computation on the request path (G1: compute lives in cmd/engine).
type Server struct {
	DB      *sql.DB
	Started time.Time
}

func New(db *sql.DB) *Server { return &Server{DB: db, Started: time.Now()} }

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /static/", s.handleStatic)
	mux.HandleFunc("GET /tools/landed-cost/{$}", s.handleToolsIndex)
	mux.HandleFunc("GET /tools/landed-cost/{corridor}", s.handleCorridor)
	mux.HandleFunc("GET /api/landedcost", s.handleLandedCostAPI)
	mux.HandleFunc("POST /api/landedcost", s.handleLandedCostAPI)
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /methodology", s.handleMethodology)
	mux.HandleFunc("GET /api/verdict", s.handleVerdict)
	mux.HandleFunc("GET /api/meta/stats", s.handleStats)
	return withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"status": "ok", "uptime_s": int(time.Since(s.Started).Seconds())})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, "home.html", map[string]any{"Title": "WatchLedger"})
}

func (s *Server) handleMethodology(w http.ResponseWriter, r *http.Request) {
	rules, _ := landedcost.ListCorridors(s.DB)
	render(w, "methodology.html", map[string]any{
		"Title":    "Methodology",
		"Ruleset":  engine.Current,
		"Version":  engine.Current.Version,
		"Hash":     engine.Current.Hash(),
		"TaxRules": rules,
		"Names":    landedcost.CountryName,
	})
}

// handleVerdict — latest content-addressed verdict for a cell under the
// current ruleset hash. 404 = we don't know yet. That is a valid answer.
func (s *Server) handleVerdict(w http.ResponseWriter, r *http.Request) {
	cellKey := strings.TrimSpace(r.URL.Query().Get("cell_key"))
	if cellKey == "" {
		writeErr(w, http.StatusBadRequest, "missing_cell_key")
		return
	}
	result, err := ledger.LoadLatestVerdictContent(s.DB, cellKey, engine.Current.Hash())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	if result == "" {
		writeErr(w, http.StatusNotFound, "no_verdict")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(result))
}

// handleToolsIndex — the corridor directory (PLAN.md §11 mechanism 1).
func (s *Server) handleToolsIndex(w http.ResponseWriter, r *http.Request) {
	rules, err := landedcost.ListCorridors(s.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	type corridorView struct {
		Slug, From, To, FromName, ToName string
		DutyRate, VatRate                string
		Stale                            bool
	}
	var corridors []corridorView
	for _, rule := range rules {
		corridors = append(corridors, corridorView{
			Slug: landedcost.SlugFor(rule.FromCountry, rule.ToCountry),
			From: rule.FromCountry, To: rule.ToCountry,
			FromName: landedcost.CountryName[rule.FromCountry],
			ToName:   landedcost.CountryName[rule.ToCountry],
			DutyRate: rule.DutyRate.StringFixed(2), VatRate: rule.VatRate.StringFixed(2),
			Stale: time.Since(rule.VerifiedAt).Hours()/24 >= landedcost.MaxRuleAgeDays,
		})
	}
	render(w, "tools.html", map[string]any{"Title": "Landed cost — what a watch really costs to import", "Corridors": corridors})
}

// handleCorridor — one canonical corridor page + worked example + editable form.
func (s *Server) handleCorridor(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("corridor")
	from, to, ok := landedcost.ParseSlug(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rule, err := landedcost.LoadRule(s.DB, from, to)
	if err != nil {
		render(w, "corridor_missing.html", map[string]any{"Title": "Corridor not covered", "Slug": slug})
		return
	}
	fx, err := landedcost.LoadFX(s.DB, r.URL.Query().Get("fx_date"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "fx_error")
		return
	}
	srcCcy, _ := landedcost.CurrencyFor(from)

	// example: a typical listing price in the source currency
	qPrice := strings.TrimSpace(r.URL.Query().Get("price"))
	if qPrice == "" {
		qPrice = "1000000"
	}
	itemPrice, perr := money.FromString(qPrice)
	var res *landedcost.Result
	var compErr string
	if perr == nil {
		shipping := money.Zero
		if qShip := strings.TrimSpace(r.URL.Query().Get("shipping")); qShip != "" {
			shipping, _ = money.FromString(qShip)
		}
		resV, err := landedcost.Compute(rule, fx, landedcost.Input{
			ItemPrice: itemPrice, Currency: srcCcy, FromCountry: from, ToCountry: to,
			Shipping: shipping,
		})
		if err != nil {
			compErr = err.Error()
		} else {
			res = &resV
		}
	} else {
		compErr = "invalid price"
	}
	stale := time.Since(rule.VerifiedAt).Hours()/24 >= landedcost.MaxRuleAgeDays
	render(w, "corridor.html", map[string]any{
		"Title":    fmt.Sprintf("Landed cost %s → %s", landedcost.CountryName[from], landedcost.CountryName[to]),
		"FromName": landedcost.CountryName[from], "ToName": landedcost.CountryName[to],
		"Slug": slug, "Price": qPrice, "SrcCurrency": srcCcy,
		"Result": res, "ComputeError": compErr,
		"DutyRate": rule.DutyRate.StringFixed(2), "VatRate": rule.VatRate.StringFixed(2),
		"VerifiedAt": rule.VerifiedAt.Format("2006-01-02"), "Stale": stale,
		"SourceURL": rule.SourceURL, "Basis": rule.Basis,
	})
}

// handleLandedCostAPI — programmatic breakdown. Pins fx_date + ruleset version.
func (s *Server) handleLandedCostAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	get := func(k string) string { return strings.TrimSpace(q.Get(k)) }
	from, to := get("from"), get("to")
	if from == "" || to == "" {
		writeErr(w, http.StatusBadRequest, "missing_from_or_to")
		return
	}
	rule, err := landedcost.LoadRule(s.DB, from, to)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_corridor")
		return
	}
	fx, err := landedcost.LoadFX(s.DB, get("fx_date"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown_fx_date")
		return
	}
	itemPrice, err := money.FromString(get("item_price"))
	if err != nil || !itemPrice.IsPositive() {
		writeErr(w, http.StatusBadRequest, "invalid_item_price")
		return
	}
	in := landedcost.Input{
		ItemPrice: itemPrice, Currency: get("currency"),
		FromCountry: from, ToCountry: to, MarginScheme: get("margin_scheme") == "true",
	}
	if c := get("currency"); c == "" {
		in.Currency, _ = landedcost.CurrencyFor(from)
	}
	if v := get("shipping"); v != "" {
		in.Shipping, _ = money.FromString(v)
	}
	if v := get("insurance_pct"); v != "" {
		in.InsurancePct, _ = money.FromString(v)
	}
	if v := get("ask_price"); v != "" {
		in.AskPrice, _ = money.FromString(v)
	}
	res, err := landedcost.Compute(rule, fx, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=21600")
	writeJSON(w, res)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var observations, verdicts int
	s.DB.QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&observations)
	s.DB.QueryRow(`SELECT COUNT(DISTINCT inputs_hash) FROM verdict_content`).Scan(&verdicts)
	sources := []map[string]any{}
	rows, err := s.DB.Query(`SELECT id, name, access_status, enabled FROM sources ORDER BY id`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, name, status string
			var enabled bool
			if rows.Scan(&id, &name, &status, &enabled) == nil {
				sources = append(sources, map[string]any{"id": id, "name": name, "access_status": status, "enabled": enabled})
			}
		}
	}
	writeJSON(w, map[string]any{
		"observations": observations,
		"verdicts":     verdicts,
		"sources":      sources,
		"ruleset":      map[string]any{"version": engine.Current.Version, "hash": engine.Current.Hash()},
	})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/static/")
	b, err := staticFS.ReadFile("static/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "application/octet-stream"
	switch {
	case strings.HasSuffix(path, ".js"):
		ct = "text/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		ct = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Write(b)
}

func render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("template %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, errCode string) {
	w.WriteHeader(code)
	writeJSON(w, map[string]string{"error": errCode})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
