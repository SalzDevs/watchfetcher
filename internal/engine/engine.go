// Package engine computes fair-value verdicts for watches using exact-cell
// matching: a verdict is derived ONLY from listings identical to the query
// across brand, model, dial, material and scope. Unknown matches unknown.
// No fallbacks, no sibling references, no guessing.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Ruleset is the versioned statistical configuration. Every verdict records
// the hash of the ruleset that produced it (PLAN.md §5: content-addressed).
// Changing any value here is a deliberate, versioned, diffed act (§13):
// bump Version, and the golden suite must be re-blessed.
type Ruleset struct {
	Version          string  `json:"version"`
	MinSamples       int     `json:"min_samples"`
	HighConfidence   int     `json:"high_confidence"`
	MediumConfidence int     `json:"medium_confidence"`
	HalfLifeDays     float64 `json:"half_life_days"`
	TrimLowMult      float64 `json:"trim_low_mult"`
	TrimHighMult     float64 `json:"trim_high_mult"`
	TrimIterations   int     `json:"trim_iterations"`
	MaxReceipts      int     `json:"max_receipts"`
	Gates            Gates   `json:"gates"`
}

// Gates are the eligibility thresholds (PLAN.md §7.4). A range publishes only
// if all four hold. Refusing to publish is the feature.
type Gates struct {
	MinVolume   int     `json:"min_volume"`    // ≥ exact-tier observations
	MinSources  int     `json:"min_sources"`   // ≥ independent sources
	MaxAgeDays  float64 `json:"max_age_days"`  // freshest observation < N days
	MaxIQRRatio float64 `json:"max_iqr_ratio"` // p75/p25 < N
}

// Current is the active ruleset. Any change = new version + golden re-bless.
var Current = Ruleset{
	Version:          "v1.0.0",
	MinSamples:       4,
	HighConfidence:   20,
	MediumConfidence: 8,
	HalfLifeDays:     900.0,
	TrimLowMult:      0.2,
	TrimHighMult:     2.2,
	TrimIterations:   2,
	MaxReceipts:      12,
	Gates:            Gates{MinVolume: 8, MinSources: 3, MaxAgeDays: 180, MaxIQRRatio: 2.5},
}

// Hash is the canonical content hash of the ruleset definition.
func (r Ruleset) Hash() string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Observation is one market data point: a listing seen at a point in time.
type Observation struct {
	Brand      string
	Model      string
	Dial       string
	Material   string
	Scope      string
	Ref        string
	Title      string
	URL        string
	Source     string
	ImageURL   string
	PriceUSD   float64
	ObservedAt time.Time
}

// Receipt is one comparable listing shown as evidence for a verdict.
type Receipt struct {
	Title    string  `json:"title"`
	Price    float64 `json:"price"`
	URL      string  `json:"url"`
	Date     string  `json:"date"`
	Source   string  `json:"source"`
	Ref      string  `json:"ref"`
	ImageURL string  `json:"image_url,omitempty"`
}

// Verdict is the precomputed fair-value answer for one exact cell.
type Verdict struct {
	CellKey    string    `json:"cell_key"`
	Brand      string    `json:"brand"`
	Model      string    `json:"model"`
	Dial       string    `json:"dial"`
	Material   string    `json:"material"`
	Scope      string    `json:"scope"`
	FairLow    float64   `json:"fair_low"`
	FairHigh   float64   `json:"fair_high"`
	Median     float64   `json:"median"`
	P25        float64   `json:"p25"`
	P75        float64   `json:"p75"`
	Count      int       `json:"count"`
	Confidence string    `json:"confidence"` // INTERNAL — never rendered
	ComputedAt time.Time `json:"computed_at"`
	Receipts   []Receipt `json:"receipts"`
	// Eligibility gates (PLAN.md §7.4). Empty = published range is gate-clean.
	GatesStatus  string   `json:"gates_status,omitempty"`  // "pass" | "limited"
	FailingGates []string `json:"failing_gates,omitempty"` // which gates failed
	RulesetHash  string   `json:"ruleset_hash,omitempty"`  // G4: content-addressed
	InputsHash   string   `json:"inputs_hash,omitempty"`   // hash of the evidence set
}

// InputsHash computes the content hash of an evidence set + cell key (G4).
// Deterministic: order-independent over observations.
func InputsHash(cellKey string, inCell []Observation) string {
	sigs := make([]string, 0, len(inCell))
	for _, o := range inCell {
		sigs = append(sigs, strings.Join([]string{
			o.Source, o.URL, o.Title, o.Ref,
			strings.TrimSpace(strconv.FormatFloat(o.PriceUSD, 'f', 2, 64)),
			strconv.FormatInt(o.ObservedAt.Unix(), 10),
		}, "\x1f"))
	}
	sort.Strings(sigs)
	h := sha256.Sum256([]byte(cellKey + "\x1e" + strings.Join(sigs, "\x1e")))
	return hex.EncodeToString(h[:])
}

// CellKey builds the exact-match identity for a watch configuration.
// Empty string = unknown, and unknown only matches unknown.
func CellKey(brand, model, dial, material, scope string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(brand)),
		strings.ToLower(strings.TrimSpace(model)),
		strings.ToLower(strings.TrimSpace(dial)),
		strings.ToLower(strings.TrimSpace(material)),
		strings.ToLower(strings.TrimSpace(scope)),
	}, "|")
}

// CellKeyForObservation is the cell an observation belongs to.
func CellKeyForObservation(o Observation) string {
	return CellKey(o.Brand, o.Model, o.Dial, o.Material, o.Scope)
}

// GroupCells buckets observations into exact cells. Observations without a
// stated brand or model are excluded — they cannot be attributed to any
// exact cell, and an unverifiable data point is not evidence.
func GroupCells(observations []Observation) map[string][]Observation {
	cells := make(map[string][]Observation)
	for _, o := range observations {
		if strings.TrimSpace(o.Brand) == "" || strings.TrimSpace(o.Model) == "" {
			continue
		}
		key := CellKeyForObservation(o)
		cells[key] = append(cells[key], o)
	}
	return cells
}

// EvaluateGates applies the eligibility gates (PLAN.md §7.4) to an exact-tier
// comparable set. Pure. Returns the failing gate names; empty = all pass.
func EvaluateGates(inCell []Observation, p25, p75 float64, now time.Time) []string {
	var failing []string
	if len(inCell) < Current.Gates.MinVolume {
		failing = append(failing, "volume")
	}
	sources := map[string]bool{}
	var freshest time.Time
	for _, o := range inCell {
		sources[o.Source] = true
		if o.ObservedAt.After(freshest) {
			freshest = o.ObservedAt
		}
	}
	if len(sources) < Current.Gates.MinSources {
		failing = append(failing, "sources")
	}
	if freshest.IsZero() || now.Sub(freshest).Hours()/24.0 >= Current.Gates.MaxAgeDays {
		failing = append(failing, "freshness")
	}
	if p25 <= 0 || p75/p25 >= Current.Gates.MaxIQRRatio {
		failing = append(failing, "dispersion")
	}
	return failing
}

// ComputeVerdict computes the fair-value verdict for one cell.
// Returns nil when the cell has fewer than Current.MinSamples observations.
func ComputeVerdict(brand, model, dial, material, scope string, observations []Observation, now time.Time) *Verdict {
	// Defense in depth: only observations belonging to the exact queried cell
	// may contribute. Wrong-cell data can never leak into a verdict.
	key := CellKey(brand, model, dial, material, scope)
	inCell := make([]Observation, 0, len(observations))
	for _, o := range observations {
		if o.PriceUSD <= 0 {
			continue
		}
		if CellKeyForObservation(o) == key {
			inCell = append(inCell, o)
		}
	}
	if len(inCell) < Current.MinSamples {
		return nil
	}
	observations = inCell

	if len(observations) < Current.MinSamples {
		return nil
	}

	// dominant-cluster trim: keep prices within [median*0.2, median*2.2],
	// re-converging over iterations; small samples pass untouched
	prices := make([]float64, 0, len(observations))
	for _, o := range observations {
		if o.PriceUSD > 0 {
			prices = append(prices, o.PriceUSD)
		}
	}
	kept := trimTypicalBand(prices)
	if len(kept) < Current.MinSamples {
		return nil
	}

	// pair kept prices with their observations for receipts + recency
	var trimmed []Observation
	keptSet := make(map[float64]int)
	for _, p := range kept {
		keptSet[p]++
	}
	for _, o := range observations {
		if o.PriceUSD > 0 && keptSet[o.PriceUSD] > 0 {
			keptSet[o.PriceUSD]--
			trimmed = append(trimmed, o)
		}
	}

	sorted := make([]float64, len(trimmed))
	for i, o := range trimmed {
		sorted[i] = o.PriceUSD
	}
	sort.Float64s(sorted)
	n := len(sorted)

	median := weightedMedian(trimmed, now)
	p25 := sorted[(n-1)/4]
	p75 := sorted[3*(n-1)/4]

	confidence := "low"
	if n >= Current.HighConfidence {
		confidence = "high"
	} else if n >= Current.MediumConfidence {
		confidence = "medium"
	}

	// receipts: the most recent Current.MaxReceipts observations
	recent := make([]Observation, len(trimmed))
	copy(recent, trimmed)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].ObservedAt.After(recent[j].ObservedAt)
	})
	receipts := make([]Receipt, 0, Current.MaxReceipts)
	for i, o := range recent {
		if i >= Current.MaxReceipts {
			break
		}
		receipts = append(receipts, Receipt{
			Title:    o.Title,
			Price:    o.PriceUSD,
			URL:      o.URL,
			Date:     o.ObservedAt.Format("2006-01"),
			Source:   o.Source,
			Ref:      o.Ref,
			ImageURL: o.ImageURL,
		})
	}

	// Eligibility gates (PLAN.md §7.4) — evaluated on the exact-tier set.
	failing := EvaluateGates(inCell, p25, p75, now)
	status := "pass"
	if len(failing) > 0 {
		status = "limited"
	}

	return &Verdict{
		GatesStatus:  status,
		FailingGates: failing,
		RulesetHash:  Current.Hash(),
		InputsHash:   InputsHash(CellKey(brand, model, dial, material, scope), inCell),
		CellKey:      CellKey(brand, model, dial, material, scope),
		Brand:        brand,
		Model:        model,
		Dial:         dial,
		Material:     material,
		Scope:        scope,
		FairLow:      p25,
		FairHigh:     p75,
		Median:       median,
		P25:          p25,
		P75:          p75,
		Count:        n,
		Confidence:   confidence,
		ComputedAt:   now,
		Receipts:     receipts,
	}
}

func weightedMedian(observations []Observation, now time.Time) float64 {
	type pair struct {
		price, weight float64
	}
	pairs := make([]pair, 0, len(observations))
	for _, o := range observations {
		ageDays := now.Sub(o.ObservedAt).Hours() / 24.0
		w := math.Pow(0.5, ageDays/Current.HalfLifeDays)
		pairs = append(pairs, pair{o.PriceUSD, w})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].price < pairs[j].price })
	total := 0.0
	for _, p := range pairs {
		total += p.weight
	}
	if total <= 0 {
		return observations[len(observations)/2].PriceUSD
	}
	cum := 0.0
	for _, p := range pairs {
		cum += p.weight
		if cum >= total/2 {
			return p.price
		}
	}
	return pairs[len(pairs)-1].price
}

func trimTypicalBand(prices []float64) []float64 {
	if len(prices) < 8 {
		return prices
	}
	sorted := make([]float64, len(prices))
	copy(sorted, prices)
	sort.Float64s(sorted)
	kept := sorted
	for i := 0; i < Current.TrimIterations; i++ {
		median := kept[len(kept)/2]
		lo, hi := median*Current.TrimLowMult, median*Current.TrimHighMult
		var next []float64
		for _, p := range kept {
			if p >= lo && p <= hi {
				next = append(next, p)
			}
		}
		if len(next) < 8 || sameFloats(next, kept) {
			kept = next
			break
		}
		kept = next
	}
	return kept
}

func sameFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
