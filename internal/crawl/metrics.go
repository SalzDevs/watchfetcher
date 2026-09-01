package crawl

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SourceStats tracks per-source crawl health for ban monitoring.
type SourceStats struct {
	Source       string `json:"source"`
	JobsTotal    int    `json:"jobs_total"`
	JobsOK       int    `json:"jobs_ok"`
	JobsErr      int    `json:"jobs_err"`
	JobsBanned   int    `json:"jobs_banned"` // jobs that hit 403/429
	Listings     int    `json:"listings"`
	HTTP200      int    `json:"http_200"`
	HTTP403      int    `json:"http_403"`
	HTTP429      int    `json:"http_429"`
	HTTP503      int    `json:"http_503"`
	Other4xx     int    `json:"other_4xx"`
	Other5xx     int    `json:"other_5xx"`
	LastError    string `json:"last_error,omitempty"`
	Banned       bool   `json:"banned"`        // true if circuit open
	Consecutive int    `json:"-"`             // internal: consecutive 403/429
}

type Metrics struct {
	mu        sync.Mutex
	StartedAt time.Time              `json:"started_at"`
	FinishedAt *time.Time            `json:"finished_at,omitempty"`
	Sources   map[string]*SourceStats `json:"sources"`
}

func NewMetrics() *Metrics {
	return &Metrics{
		StartedAt: time.Now().UTC(),
		Sources:   make(map[string]*SourceStats),
	}
}

func (m *Metrics) get(source string) *SourceStats {
	if s, ok := m.Sources[source]; ok {
		return s
	}
	s := &SourceStats{Source: source}
	m.Sources[source] = s
	return s
}

func (m *Metrics) RecordJobStart(source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.get(source)
	s.JobsTotal++
}

func (m *Metrics) RecordJobOK(source string, listings int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.get(source)
	s.JobsOK++
	s.Listings += listings
	s.Consecutive = 0
	s.Banned = false
}

func (m *Metrics) RecordJobErr(source string, errStr string, banned bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.get(source)
	s.JobsErr++
	s.LastError = errStr
	if banned {
		s.JobsBanned++
		s.Consecutive++
		if s.Consecutive >= 2 {
			s.Banned = true
		}
	} else {
		// Non-ban error resets consecutive only after success; keep count
		// but don't mark banned.
	}
}

func (m *Metrics) RecordHTTP(source string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.get(source)
	switch status {
	case 200, 302, 303:
		s.HTTP200++
		s.Consecutive = 0
		s.Banned = false
	case 403:
		s.HTTP403++
		s.Consecutive++
		if s.Consecutive >= 2 {
			s.Banned = true
		}
	case 429:
		s.HTTP429++
		s.Consecutive++
		if s.Consecutive >= 2 {
			s.Banned = true
		}
	case 503:
		s.HTTP503++
	default:
		if status >= 400 && status < 500 {
			s.Other4xx++
		} else if status >= 500 {
			s.Other5xx++
		}
	}
	// Any 2xx resets is handled above; 429/403 keep accumulating until success.
}

func (m *Metrics) IsBanned(source string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Sources[source]; ok {
		return s.Banned
	}
	return false
}

// CircuitOpen reports whether further jobs for source should be skipped.
func (m *Metrics) CircuitOpen(source string) bool {
	return m.IsBanned(source)
}

func (m *Metrics) Finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	m.FinishedAt = &now
}

// Summary returns human-readable one-liner per source for logs.
func (m *Metrics) Summary() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := ""
	for _, s := range m.Sources {
		banMark := ""
		if s.Banned {
			banMark = " [BANNED?]"
		}
		out += fmt.Sprintf("%s: %d ok / %d err (%d listings) 200:%d 403:%d 429:%d 503:%d%s last_err=%q\n",
			s.Source, s.JobsOK, s.JobsErr, s.Listings, s.HTTP200, s.HTTP403, s.HTTP429, s.HTTP503, banMark, s.LastError)
	}
	return out
}

// WriteJSON writes metrics to path atomically (temp + rename) for monitoring.
func (m *Metrics) WriteJSON(path string) error {
	m.mu.Lock()
	clone := struct {
		StartedAt  time.Time               `json:"started_at"`
		FinishedAt *time.Time              `json:"finished_at,omitempty"`
		Sources    map[string]*SourceStats `json:"sources"`
		DurationS  float64                 `json:"duration_s"`
	}{
		StartedAt: m.StartedAt,
		FinishedAt: m.FinishedAt,
		Sources:   m.Sources,
	}
	if m.FinishedAt != nil {
		clone.DurationS = m.FinishedAt.Sub(m.StartedAt).Seconds()
	} else {
		clone.DurationS = time.Since(m.StartedAt).Seconds()
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(clone, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AlertString returns a non-empty string if any source looks banned — suitable for stderr.
func (m *Metrics) AlertString() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	alert := ""
	for _, s := range m.Sources {
		if s.Banned || s.HTTP403 >= 3 || s.HTTP429 >= 3 {
			alert += fmt.Sprintf("ALERT: %s may be banning us (403=%d 429=%d consecutive=%d) — check logs, will auto-retry next run\n",
				s.Source, s.HTTP403, s.HTTP429, s.Consecutive)
		}
		if s.Source == "chrono24" && s.JobsErr > 0 && s.JobsOK == 0 {
			alert += fmt.Sprintf("ALERT: chrono24 completely failed (%d err, last: %q) — likely IP flagged by WAF\n",
				s.JobsErr, s.LastError)
		}
	}
	return alert
}

var GlobalMetrics *Metrics

func init() {
	GlobalMetrics = NewMetrics()
}

// SetGlobal sets the metrics instance used by polite helpers when m == nil.
func SetGlobal(m *Metrics) { GlobalMetrics = m }

func globalOr(m *Metrics) *Metrics {
	if m != nil {
		return m
	}
	return GlobalMetrics
}
