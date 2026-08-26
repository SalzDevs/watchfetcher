// Package engine computes fair-value verdicts for watches using exact-cell
// matching: a verdict is derived ONLY from listings identical to the query
// across brand, model, dial, material and scope. Unknown matches unknown.
// No fallbacks, no sibling references, no guessing.
package engine

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Frozen spec constants.
const (
	MinSamples        = 4     // below this: no verdict, ever
	HighConfidence    = 20    // internal quality tiers — never shown to users
	MediumConfidence  = 8     //
	HalfLifeDays      = 900.0 // recency weighting: a sale from 900 days ago counts half
	TrimLowMult       = 0.2   // dominant-cluster trim band
	TrimHighMult      = 2.2
	TrimIterations    = 2
	MaxReceipts       = 12
)

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
	PriceUSD   float64
	ObservedAt time.Time
}

// Receipt is one comparable listing shown as evidence for a verdict.
type Receipt struct {
	Title  string  `json:"title"`
	Price  float64 `json:"price"`
	URL    string  `json:"url"`
	Date   string  `json:"date"`
	Source string  `json:"source"`
	Ref    string  `json:"ref"`
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

// ComputeVerdict computes the fair-value verdict for one cell.
// Returns nil when the cell has fewer than MinSamples observations.
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
	if len(inCell) < MinSamples {
		return nil
	}
	observations = inCell

	if len(observations) < MinSamples {
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
	if len(kept) < MinSamples {
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
	if n >= HighConfidence {
		confidence = "high"
	} else if n >= MediumConfidence {
		confidence = "medium"
	}

	// receipts: the most recent MaxReceipts observations
	recent := make([]Observation, len(trimmed))
	copy(recent, trimmed)
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].ObservedAt.After(recent[j].ObservedAt)
	})
	receipts := make([]Receipt, 0, MaxReceipts)
	for i, o := range recent {
		if i >= MaxReceipts {
			break
		}
		receipts = append(receipts, Receipt{
			Title:  o.Title,
			Price:  o.PriceUSD,
			URL:    o.URL,
			Date:   o.ObservedAt.Format("2006-01"),
			Source: o.Source,
			Ref:    o.Ref,
		})
	}

	return &Verdict{
		CellKey:    CellKey(brand, model, dial, material, scope),
		Brand:      brand,
		Model:      model,
		Dial:       dial,
		Material:   material,
		Scope:      scope,
		FairLow:    p25,
		FairHigh:   p75,
		Median:     median,
		P25:        p25,
		P75:        p75,
		Count:      n,
		Confidence: confidence,
		ComputedAt: now,
		Receipts:   receipts,
	}
}

func weightedMedian(observations []Observation, now time.Time) float64 {
	type pair struct {
		price, weight float64
	}
	pairs := make([]pair, 0, len(observations))
	for _, o := range observations {
		ageDays := now.Sub(o.ObservedAt).Hours() / 24.0
		w := math.Pow(0.5, ageDays/HalfLifeDays)
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
	for i := 0; i < TrimIterations; i++ {
		median := kept[len(kept)/2]
		lo, hi := median*TrimLowMult, median*TrimHighMult
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
