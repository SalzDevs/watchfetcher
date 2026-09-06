// Package httpx: server-rendered HTTP surface. html/template + htmx — no SPA,
// every evidence page readable without JS (trust product, SEO-native).
package httpx

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"watchledger/internal/auth"
	"watchledger/internal/catalogue"
	"watchledger/internal/engine"
	"watchledger/internal/evaluate"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var tpl = template.Must(template.New("").Funcs(template.FuncMap{
	"json": func(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) },
	"time": func(ts int64) time.Time { return time.Unix(ts, 0) },
	"truncate": func(n int, s string) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "…"
	},
}).ParseFS(templatesFS, "templates/*.html"))

// Server holds read-model dependencies. Routes serve precomputed data only —
// no live computation on the request path (G1: compute lives in cmd/engine).
type Server struct {
	DB      *sql.DB
	Mailer  auth.Mailer
	BaseURL string
	Started time.Time
}

func New(db *sql.DB) *Server {
	return &Server{DB: db, Mailer: &auth.LogMailer{Prefix: "[dev-mail]"}, BaseURL: "http://localhost:8080", Started: time.Now()}
}

// SetMail — production wiring (SMTP/provider); dev default is LogMailer.
func (s *Server) SetMail(m auth.Mailer, baseURL string) { s.Mailer = m; s.BaseURL = baseURL }

// currentUser — session cookie → (userID, email, ok).
func (s *Server) currentUser(r *http.Request) (int64, string, bool) {
	c, err := r.Cookie("wl_session")
	if err != nil {
		return 0, "", false
	}
	return auth.User(s.DB, c.Value)
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /static/", s.handleStatic)
	mux.HandleFunc("GET /tools/landed-cost/{$}", s.handleToolsIndex)
	mux.HandleFunc("GET /tools/landed-cost/{corridor}", s.handleCorridor)
	mux.HandleFunc("GET /api/landedcost", s.handleLandedCostAPI)
	mux.HandleFunc("POST /api/landedcost", s.handleLandedCostAPI)
	mux.HandleFunc("GET /evaluate", s.handleEvaluateForm)
	mux.HandleFunc("GET /evaluate/{$}", s.handleEvaluateForm)
	mux.HandleFunc("POST /evaluate", s.handleEvaluateSubmit)
	mux.HandleFunc("POST /evaluate/{$}", s.handleEvaluateSubmit)
	mux.HandleFunc("GET /evaluate/{id}", s.handleEvaluation)
	mux.HandleFunc("GET /evaluate/{id}/brief", s.handleBrief)
	mux.HandleFunc("GET /references/{ref}", s.handleReference)
	mux.HandleFunc("GET /references/{$}", s.handleReferencesIndex)
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLoginStart)
	mux.HandleFunc("GET /login/confirm", s.handleLoginConfirm)
	mux.HandleFunc("GET /logout", s.handleLogout)
	mux.HandleFunc("GET /watchlist", s.requireUser(s.handleWatchlist))
	mux.HandleFunc("POST /watchlist/add", s.requireUser(s.handleWatchAdd))
	mux.HandleFunc("POST /watchlist/remove", s.requireUser(s.handleWatchRemove))
	mux.HandleFunc("POST /reports/submit", s.requireUser(s.handleReportSubmit))
	mux.HandleFunc("GET /admin/reports", s.requireAdmin(s.handleReportsAdmin))
	mux.HandleFunc("POST /admin/reports/verify", s.requireAdmin(s.handleReportVerify))
	mux.HandleFunc("GET /google317ab87a292b5275.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "google-site-verification: google317ab87a292b5275.html")
	})
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /ebay/notifications", s.handleEbayChallenge)
	mux.HandleFunc("POST /ebay/notifications", s.handleEbayDeletion)
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

// familyScope — Phase 2 scope discipline (PLAN.md §10): five families only.
var familyScope = map[string]bool{
	"Submariner Date":          true,
	"Submariner No-Date":       true,
	"Datejust":                 true,
	"Speedmaster Professional": true,
	"Black Bay":                true,
	"Black Bay 58":             true,
}

type referenceView struct {
	Ref, Brand, Family, Dial, Material string
	InScope                            bool
	SoldObservations, SoldTotal        int
}

func (s *Server) handleReferencesIndex(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`SELECT ref, brand, family, dial, material FROM catalogue_references ORDER BY brand, family, ref`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()
	var refs []referenceView
	for rows.Next() {
		var v referenceView
		if rows.Scan(&v.Ref, &v.Brand, &v.Family, &v.Dial, &v.Material) == nil {
			v.InScope = familyScope[v.Family]
			refs = append(refs, v)
		}
	}
	render(w, "references.html", map[string]any{"Title": "References", "References": refs})
}

// handleReference — the permanent evidence page (PLAN.md §9 Phase 2).
// Realised block = auction_realised observations for this ref, exact tier only.
// Gate-failing refs render counts, never a range (G5/G9).
func (s *Server) handleReference(w http.ResponseWriter, r *http.Request) {
	input := r.PathValue("ref")
	res, err := catalogue.Lookup(s.DB, input)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	if res.NeedsReview || res.Ref == "" {
		render(w, "reference_uncatalogued.html", map[string]any{"Title": "Reference not covered", "Ref": input})
		return
	}
	if !familyScope[res.Family] {
		render(w, "reference_uncatalogued.html", map[string]any{
			"Title": "Not yet covered", "Ref": res.Ref,
			"Message": fmt.Sprintf("%s %s is outside the Phase 2 scope — five families only while the evidence base is built.", res.Brand, res.Family),
		})
		return
	}

	obs, err := s.refObservations(res)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}

	page := map[string]any{
		"Title": fmt.Sprintf("%s %s %s", res.Brand, res.Family, res.Ref),
		"Ref":   res.Ref, "Brand": res.Brand, "Family": res.Family,
		"Dial": dialOr(res.Dial), "Material": matOr(res.Material),
		"Verdict": (*engine.Verdict)(nil), "Count": len(obs),
	}
	if len(obs) > 0 {
		v := engine.ComputeVerdict(res.Brand, res.Family, res.Dial, res.Material, "", obs, time.Now().UTC())
		if v != nil {
			page["Verdict"] = v
			page["Count"] = v.Count
		}
		page["Evidence"] = evidenceRows(obs)

		// Phase 3: asks vs realised spread — published only when the realised
		// side passes gates (a spread built on thin data is a claim we refuse,
		// PLAN.md §3/§7.4).
		if v.GatesStatus == "pass" {
			if asks, n := s.refAsks(res); n > 0 {
				askMedian := medianAsks(asks)
				if askMedian.IsPositive() {
					spread := askMedian.Sub(v.Median).Div(v.Median).Mul(money.MustDecimal("100")).Round(1)
					page["AskMedian"] = askMedian.StringFixed(0)
					page["AskCount"] = n
					page["SpreadPct"] = spread.StringFixed(1)
				}
			}
		}
	}
	// family navigation (the vault, simplified)
	siblings, _ := catalogue.FamilyRefs(s.DB, res.Brand, res.Family)
	page["Siblings"] = siblings

	// phase 5: personal surfaces (authed) vs public evidence
	userID, email, authed := s.currentUser(r)
	page["Authed"] = authed
	page["Email"] = email
	if authed {
		page["Watching"] = store.Watching(s.DB, userID, res.Ref)
	}

	render(w, "reference.html", page)
}

type evidenceRow struct {
	Source, Date, Title string
	Price               string
	URL                 string
}

func (s *Server) refObservations(res catalogue.Resolution) ([]engine.Observation, error) {
	rows, err := s.DB.Query(`
		SELECT source_id, title, url, price_usd, observed_at
		FROM observations
		WHERE kind = 'auction_realised' AND UPPER(ref) = UPPER(?)
		ORDER BY observed_at DESC`, res.Ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Observation
	for rows.Next() {
		var o engine.Observation
		var price string
		var ts int64
		if err := rows.Scan(&o.Source, &o.Title, &o.URL, &price, &ts); err != nil {
			return nil, err
		}
		d, err := money.FromString(price)
		if err != nil {
			continue
		}
		// cell identity comes from the catalogue resolution — the observation
		// row is ref-keyed, the engine needs the full 5-tuple
		o.Brand, o.Model, o.Dial, o.Material = res.Brand, res.Family, res.Dial, res.Material
		o.Ref = res.Ref
		o.PriceUSD = d
		o.ObservedAt = time.Unix(ts, 0)
		out = append(out, o)
	}
	return out, rows.Err()
}

func evidenceRows(obs []engine.Observation) []evidenceRow {
	out := make([]evidenceRow, 0, len(obs))
	for _, o := range obs {
		out = append(out, evidenceRow{
			Source: o.Source, Date: o.ObservedAt.Format("2006-01-02"),
			Title: o.Title, Price: o.PriceUSD.StringFixed(2), URL: o.URL,
		})
	}
	return out
}

// refAsks — current asks for this ref (kind='ask', last 90 days).
// asker = weighted median over the ask set; nil = spread block hidden.
func (s *Server) refAsks(res catalogue.Resolution) ([]engine.Observation, int) {
	rows, err := s.DB.Query(`
		SELECT source_id, title, url, price_usd, observed_at
		FROM observations
		WHERE kind = 'ask' AND UPPER(ref) = UPPER(?)
		  AND observed_at >= strftime('%s','now','-90 days')`, res.Ref)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	var out []engine.Observation
	for rows.Next() {
		var o engine.Observation
		var price string
		var ts int64
		if rows.Scan(&o.Source, &o.Title, &o.URL, &price, &ts) != nil {
			continue
		}
		d, err := money.FromString(price)
		if err != nil || !d.IsPositive() {
			continue
		}
		o.PriceUSD = d
		o.ObservedAt = time.Unix(ts, 0)
		out = append(out, o)
	}
	return out, len(out)
}

var asker = medianAsks

func medianAsks(asks []engine.Observation) money.Decimal {
	if len(asks) == 0 {
		return money.Zero
	}
	ds := make([]money.Decimal, 0, len(asks))
	for _, o := range asks {
		ds = append(ds, o.PriceUSD)
	}
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j].LessThan(ds[j-1]); j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
	return ds[len(ds)/2]
}

// ---- Phase 4: the evaluator (PLAN.md §9: the core job) ----

// handleEvaluateForm — manual entry is first-class (PLAN.md §9), never a
// degraded fallback. Refs prefill from the catalogue; unsupported listing
// URLs route here with the title prefilled, no error shaming.
func (s *Server) handleEvaluateForm(w http.ResponseWriter, r *http.Request) {
	refs, _ := catalogue.FamilyRefs(s.DB, "", "") // all refs
	if refs == nil {
		rows, err := s.DB.Query(`SELECT ref FROM catalogue_references ORDER BY ref`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var ref string
				if rows.Scan(&ref) == nil {
					refs = append(refs, ref)
				}
			}
		}
	}
	corridors, _ := landedcost.ListCorridors(s.DB)
	type corridorView struct{ From, To, Slug, FromName, ToName string }
	var corr []corridorView
	seen := map[string]bool{}
	for _, rule := range corridors {
		slug := landedcost.SlugFor(rule.FromCountry, rule.ToCountry)
		if seen[rule.ToCountry] {
			continue
		}
		seen[rule.ToCountry] = true
		corr = append(corr, corridorView{
			Slug: slug, From: rule.FromCountry, To: rule.ToCountry,
			FromName: landedcost.CountryName[rule.FromCountry],
			ToName:   landedcost.CountryName[rule.ToCountry],
		})
	}
	render(w, "evaluate_form.html", map[string]any{
		"Title": "Evaluate a listing", "Refs": refs, "Corridors": corr,
		"PrefillRef":   strings.TrimSpace(r.URL.Query().Get("ref")),
		"PrefillTitle": strings.TrimSpace(r.URL.Query().Get("title")),
	})
}

// handleEvaluateSubmit — run the composition, store content-addressed,
// redirect to the permalink.
func (s *Server) handleEvaluateSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_form")
		return
	}
	get := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	in := evaluate.Input{Now: time.Now().UTC(), URL: get("url")}
	in.RefInput = get("ref")
	if p, err := money.FromString(get("price")); err == nil {
		in.Price = p
	}
	in.Currency = get("currency")
	if in.Currency == "" {
		in.Currency = "USD"
	}
	in.ToCountry = get("to_country")
	in.MarginScheme = get("margin_scheme") == "on"

	// the resolver cascade handles sub-0.85 by asking for confirmation on the
	// result page — the evaluation still stores (needs_review state)
	res, err := evaluate.Evaluate(s.DB, in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := evaluate.SaveEvaluation(s.DB, res)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/evaluate/%d", id), http.StatusSeeOther)
}

// handleEvaluation — the permalink. Everything on this page re-derives.
func (s *Server) handleEvaluation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	res, err := evaluate.LoadEvaluation(s.DB, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	if res == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "evaluation.html", map[string]any{"Title": "Evaluation — " + res.Ref, "E": res, "EvalID": id})
}

// handleBrief — the negotiation evidence sheet (PLAN.md §11 mechanism 2):
// print-CSS one-pager, no PDF service — the user prints/saves.
func (s *Server) handleBrief(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	res, err := evaluate.LoadEvaluation(s.DB, id)
	if err != nil || res == nil {
		http.NotFound(w, r)
		return
	}
	render(w, "brief.html", map[string]any{"Title": "Evidence sheet — " + res.Ref, "E": res})
}

// ---- auth + phase 5 surfaces ----

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _, ok := s.currentUser(r)
		if !ok {
			http.Redirect(w, r, "/login?next="+urlQueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(r.Context()))
		_ = id
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	admin := stringsToLower(osGetenv("ADMIN_EMAIL"))
	return func(w http.ResponseWriter, r *http.Request) {
		_, email, ok := s.currentUser(r)
		if !ok || admin == "" || !stringsEqualFold(email, admin) {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}

func urlQueryEscape(s string) string           { return strings_ReplaceAll(s, "&", "%26") }
func strings_ReplaceAll(s, o, n string) string { return strings.ReplaceAll(s, o, n) }
func stringsToLower(s string) string           { return strings.ToLower(s) }
func stringsEqualFold(a, b string) bool        { return strings.EqualFold(a, b) }
func osGetenv(k string) string                 { return os.Getenv(k) }

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	render(w, "login.html", map[string]any{
		"Title": "Sign in", "Next": strings.TrimSpace(r.URL.Query().Get("next")),
		"Sent": false,
	})
}

func (s *Server) handleLoginStart(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	if err := auth.StartLogin(s.DB, s.Mailer, email, s.BaseURL); err != nil {
		render(w, "login.html", map[string]any{"Title": "Sign in", "Error": err.Error(), "Sent": false})
		return
	}
	render(w, "login.html", map[string]any{"Title": "Check your email", "Sent": true, "Email": email})
}

func (s *Server) handleLoginConfirm(w http.ResponseWriter, r *http.Request) {
	session, err := auth.ConfirmLogin(s.DB, strings.TrimSpace(r.URL.Query().Get("token")))
	if err != nil {
		render(w, "login.html", map[string]any{"Title": "Sign in", "Error": err.Error(), "Sent": false})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "wl_session", Value: session, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(auth.SessionTTL.Seconds()),
	})
	next := strings.TrimSpace(r.URL.Query().Get("next"))
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/watchlist"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("wl_session"); err == nil {
		auth.Logout(s.DB, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "wl_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleWatchlist — one row per watched reference: evidence state + controls.
func (s *Server) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := s.currentUser(r)
	rows, err := store.WatchlistFor(s.DB, userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	type watchView struct {
		Ref          string
		Realised     int
		LastObserved string
		GatesStatus  string
		HasData      bool
	}
	var views []watchView
	for _, w := range rows {
		v := watchView{Ref: w.Ref}
		res, err := catalogue.Lookup(s.DB, w.Ref)
		if err == nil && !res.NeedsReview {
			obs, err := s.refObservations(res)
			if err == nil && len(obs) > 0 {
				v.HasData = true
				v.Realised = len(obs)
				last := obs[0].ObservedAt
				for _, o := range obs {
					if o.ObservedAt.After(last) {
						last = o.ObservedAt
					}
				}
				v.LastObserved = last.Format("2006-01-02")
				if vd := engine.ComputeVerdict(res.Brand, res.Family, res.Dial, res.Material, "", obs, time.Now().UTC()); vd != nil {
					v.GatesStatus = vd.GatesStatus
				}
			}
		}
		views = append(views, v)
	}
	render(w, "watchlist.html", map[string]any{"Title": "Your watchlist", "Rows": views})
}

func (s *Server) handleWatchAdd(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := s.currentUser(r)
	ref := strings.TrimSpace(r.FormValue("ref"))
	back := strings.TrimSpace(r.FormValue("back"))
	if ref == "" {
		http.Redirect(w, r, backOr(back), http.StatusSeeOther)
		return
	}
	store.WatchAdd(s.DB, userID, ref)
	http.Redirect(w, r, backOr(back), http.StatusSeeOther)
}

func (s *Server) handleWatchRemove(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := s.currentUser(r)
	store.WatchRemove(s.DB, userID, strings.TrimSpace(r.FormValue("ref")))
	http.Redirect(w, r, "/watchlist", http.StatusSeeOther)
}

func backOr(b string) string {
	if b != "" && strings.HasPrefix(b, "/") {
		return b
	}
	return "/watchlist"
}

// handleReportSubmit — user-reported paid price (Tier 3). Review before ledger.
func (s *Server) handleReportSubmit(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := s.currentUser(r)
	ref := strings.TrimSpace(r.FormValue("ref"))
	price := strings.TrimSpace(r.FormValue("price"))
	currency := strings.TrimSpace(r.FormValue("currency"))
	if ref == "" || price == "" {
		http.Redirect(w, r, "/references", http.StatusSeeOther)
		return
	}
	if currency == "" {
		currency = "USD"
	}
	paidAt := time.Now().UTC()
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(r.FormValue("paid_at"))); err == nil {
		paidAt = d
	}
	if _, err := store.AddPriceReport(s.DB, userID, ref, price, currency, paidAt); err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	back := strings.TrimSpace(r.FormValue("back"))
	if back == "" {
		back = "/watchlist"
	}
	http.Redirect(w, r, back+"?reported=1", http.StatusSeeOther)
}

// handleReportsAdmin — the human gate (PLAN.md §5.4: nothing renders until verified).
func (s *Server) handleReportsAdmin(w http.ResponseWriter, r *http.Request) {
	reports, err := store.OpenPriceReports(s.DB)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db_error")
		return
	}
	render(w, "reports_admin.html", map[string]any{"Title": "Price reports review", "Reports": reports})
}

// handleReportVerify — verified reports append to the ledger as
// kind='user_reported' (distinct tier, never merged into auction realised).
func (s *Server) handleReportVerify(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	action := r.FormValue("action")
	_, email, _ := s.currentUser(r)
	if action == "verify" {
		var ref, price, currency string
		var paidAt int64
		if err := s.DB.QueryRow(`SELECT ref, price, currency, paid_at FROM price_reports WHERE id = ?`, id).
			Scan(&ref, &price, &currency, &paidAt); err != nil {
			http.NotFound(w, r)
			return
		}
		usd := price
		if currency != "USD" {
			if fx, err := landedcost.LoadFX(s.DB, "latest"); err == nil {
				if d, err := money.FromString(price); err == nil {
					if c, err := landedcost.ConvertToUSD(fx, currency, d); err == nil {
						usd = c.StringFixed(2)
					}
				}
			}
		}
		if _, err := ledger.AppendObservation(s.DB, ledger.Observation{
			SourceID: "user_reported", Kind: "user_reported",
			Ref: ref, Title: "user-reported paid price (reviewed by " + email + ")",
			URL: "", Price: price, Currency: currency, PriceUSD: usd,
			ObservedAt: time.Unix(paidAt, 0),
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	store.SetReportStatus(s.DB, id, action, email)
	http.Redirect(w, r, "/admin/reports", http.StatusSeeOther)
}

// handleSitemap — corridor + reference URLs (PLAN.md §11: the pages ARE the
// marketing; indexable body deep-not-broad). Only in-scope families + corridors.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	base := s.BaseURL
	if base == "" || strings.Contains(base, "localhost") {
		base = "https://" + r.Host
	}
	var urls []string
	for _, p := range []string{"/", "/methodology", "/tools/landed-cost", "/evaluate", "/references"} {
		urls = append(urls, base+p)
	}
	if rules, err := landedcost.ListCorridors(s.DB); err == nil {
		for _, rule := range rules {
			urls = append(urls, base+"/tools/landed-cost/"+landedcost.SlugFor(rule.FromCountry, rule.ToCountry))
		}
	}
	rows, err := s.DB.Query(`SELECT ref FROM catalogue_references WHERE family IN
		('Submariner Date','Submariner No-Date','Datejust','Speedmaster Professional','Black Bay','Black Bay 58')`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ref string
			if rows.Scan(&ref) == nil {
				urls = append(urls, base+"/references/"+ref)
			}
		}
	}
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, u := range urls {
		fmt.Fprintf(w, "  <url><loc>%s</loc></url>\n", u)
	}
	fmt.Fprintf(w, "</urlset>\n")
}

func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	base := s.BaseURL
	if base == "" || strings.Contains(base, "localhost") {
		base = "https://" + r.Host
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", base)
}

// ---- eBay compliance endpoints (production key set requirement) ----

// handleEbayChallenge — per eBay's Marketplace Account Deletion spec:
// GET ?challenge_code=<unique> must respond 200, application/json with
// {"challengeResponse": hex(sha256(challenge_code + verificationToken + endpoint))}
// — parameters concatenated in exactly that order.
func (s *Server) handleEbayChallenge(w http.ResponseWriter, r *http.Request) {
	challengeCode := r.URL.Query().Get("challenge_code")
	if challengeCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	token := os.Getenv("EBAY_VERIFICATION_TOKEN")
	endpoint := os.Getenv("EBAY_NOTIFICATION_ENDPOINT")
	if endpoint == "" {
		endpoint = "https://watchfairvalue.com/ebay/notifications"
	}
	if token == "" {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "verification token not configured")
		return
	}
	h := sha256.New()
	h.Write([]byte(challengeCode))
	h.Write([]byte(token))
	h.Write([]byte(endpoint))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"challengeResponse": hex.EncodeToString(h.Sum(nil))})
}

// handleEbayDeletion — signed account-deletion notifications (GDPR/CCPA).
// Archive payload as a raw document (G3 provenance, compliance record) and
// acknowledge. JWS signature verification with eBay's public cert = hardening
// TODO; payload carries no personal data we hold meanwhile.
func (s *Server) handleEbayDeletion(w http.ResponseWriter, r *http.Request) {
	body, err := ioReadAll(r)
	if err != nil || len(body) == 0 || len(body) > 10<<20 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Spec: acknowledge immediately (200/201/202/204). Authenticity is
	// verified via the X-EBAY-SIGNATURE JWS header + eBay's public key
	// (getPublicKey API) — hardening TODO; the token is only used in the
	// challenge computation, never in the POST payload.
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	s.DB.Exec(`INSERT OR IGNORE INTO raw_documents (source_id, url, fetched_at, content_hash, content_type, body)
		VALUES ('ebay', 'marketplace-account-deletion', strftime('%s','now'), ?, 'application/json', ?)`, hash, string(body))
	w.WriteHeader(http.StatusOK)
}

func ioReadAll(r *http.Request) ([]byte, error) {
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}

func dialOr(s string) string {
	if s == "" {
		return "unspecified"
	}
	return s
}
func matOr(s string) string {
	if s == "" {
		return "unspecified"
	}
	return s
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
