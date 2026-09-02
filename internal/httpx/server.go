// Package httpx: server-rendered HTTP surface. html/template + htmx — no SPA,
// every evidence page readable without JS (trust product, SEO-native).
package httpx

import (
	"database/sql"
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/ledger"
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
	render(w, "methodology.html", map[string]any{
		"Title":   "Methodology",
		"Ruleset": engine.Current,
		"Version": engine.Current.Version,
		"Hash":    engine.Current.Hash(),
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
