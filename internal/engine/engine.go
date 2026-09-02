// Package engine: pure, deterministic verdict computation. No I/O, no network,
// no clock — time enters as an argument (PLAN.md §5 layering rule, G1).
//
// Money is decimal.Decimal end to end (G7). Every verdict records the hash of
// the ruleset and the hash of its evidence set (G4): same evidence + same
// rules = same verdict, forever.
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

	"watchledger/internal/money"
)

// Ruleset is the versioned statistical configuration. Changing any value is a
// deliberate act: bump Version, re-bless the golden suite (PLAN.md §13).
type Ruleset struct {
	Version          string  `json:"version"`
	MinSamples       int     `json:"min_samples"`     // below: no computation at all
	HighConfidence   int     `json:"high_confidence"` // internal tiers, never rendered
	MediumConfidence int     `json:"medium_confidence"`
	HalfLifeDays     float64 `json:"half_life_days"` // recency weighting (not money — float ok)
	TrimLowMult      float64 `json:"trim_low_mult"`  // dominant-cluster band, as multipliers of median
	TrimHighMult     float64 `json:"trim_high_mult"`
	TrimIterations   int     `json:"trim_iterations"`
	MaxReceipts      int     `json:"max_receipts"`
	Gates            Gates   `json:"gates"`
}

// Gates — the eligibility thresholds (PLAN.md §7.4). A range publishes only
// if all four hold. Refusing to publish is the feature (§1).
type Gates struct {
	MinVolume   int     `json:"min_volume"`    // ≥ exact-tier observations
	MinSources  int     `json:"min_sources"`   // ≥ independent sources
	MaxAgeDays  float64 `json:"max_age_days"`  // freshest observation < N days old
	MaxIQRRatio float64 `json:"max_iqr_ratio"` // p75 / p25 < N
}

// Current is the active ruleset. One row in rulesets table per distinct hash.
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

// Hash — canonical content hash of the definition (G4).
func (r Ruleset) Hash() string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Observation is one append-only ledger price event, normalised to USD.
type Observation struct {
	Brand      string
	Model      string
	Dial       string
	Material   string
	Scope      string
	Ref        string
	Kind       string // ask | sold | auction_realised | user_reported
	Source     string
	Title      string
	URL        string
	PriceUSD   money.Decimal
	ObservedAt time.Time
}

// Receipt is one comparable shown as evidence for a verdict.
type Receipt struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Ref      string `json:"ref"`
	Date     string `json:"date"`      // YYYY-MM
	PriceUSD string `json:"price_usd"` // decimal string (G7 — money never a JSON number)
}

// Verdict is the computed answer for one exact cell.
type Verdict struct {
	CellKey      string        `json:"cell_key"`
	Brand        string        `json:"brand"`
	Model        string        `json:"model"`
	Dial         string        `json:"dial"`
	Material     string        `json:"material"`
	Scope        string        `json:"scope"`
	P10          money.Decimal `json:"p10"`
	Median       money.Decimal `json:"median"`
	P90          money.Decimal `json:"p90"`
	Count        int           `json:"count"`
	Confidence   string        `json:"confidence"` // internal — never rendered
	ComputedAt   time.Time     `json:"computed_at"`
	Receipts     []Receipt     `json:"receipts"`
	GatesStatus  string        `json:"gates_status"`            // "pass" | "limited"
	FailingGates []string      `json:"failing_gates,omitempty"` // which gates failed
	RulesetHash  string        `json:"ruleset_hash"`
	InputsHash   string        `json:"inputs_hash"`
}

// CellKey — the exact-match identity of a watch configuration.
// Empty string = unknown, and unknown only matches unknown.
func CellKey(brand, model, dial, material, scope string) string {
	parts := []string{brand, model, dial, material, scope}
	for i, p := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(p))
	}
	return strings.Join(parts, "|")
}

// InputsHash — content hash of an evidence set + cell key (G4).
// Order-independent: the ledger never promises row order.
func InputsHash(cellKey string, inCell []Observation) string {
	sigs := make([]string, 0, len(inCell))
	for _, o := range inCell {
		sigs = append(sigs, strings.Join([]string{
			o.Source, o.URL, o.Title, o.Ref, o.Kind,
			o.PriceUSD.String(),
			strconv.FormatInt(o.ObservedAt.Unix(), 10),
		}, "\x1f"))
	}
	sort.Strings(sigs)
	sum := sha256.Sum256([]byte(cellKey + "\x1e" + strings.Join(sigs, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// EvaluateGates — PLAN.md §7.4. Pure. Returns failing gate names; empty = pass.
func EvaluateGates(inCell []Observation, p25, p75 money.Decimal, now time.Time) []string {
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
	if p25.IsPositive() {
		ratio := p75.Div(p25)
		if ratio.GreaterThanOrEqual(money.MustDecimal(strconv.FormatFloat(Current.Gates.MaxIQRRatio, 'f', -1, 64))) {
			failing = append(failing, "dispersion")
		}
	} else {
		failing = append(failing, "dispersion")
	}
	return failing
}

// ComputeVerdict computes the verdict for one exact cell from the given
// evidence set. Returns nil below MinSamples (we do not compute, therefore
// we do not accidentally publish).
func ComputeVerdict(brand, model, dial, material, scope string, observations []Observation, now time.Time) *Verdict {
	key := CellKey(brand, model, dial, material, scope)
	inCell := make([]Observation, 0, len(observations))
	for _, o := range observations {
		if !o.PriceUSD.IsPositive() {
			continue
		}
		if CellKey(o.Brand, o.Model, o.Dial, o.Material, o.Scope) == key {
			inCell = append(inCell, o)
		}
	}
	if len(inCell) < Current.MinSamples {
		return nil
	}

	prices := make([]money.Decimal, 0, len(inCell))
	for _, o := range inCell {
		prices = append(prices, o.PriceUSD)
	}
	kept := trimTypicalBand(prices)
	if len(kept) < Current.MinSamples {
		return nil
	}

	// pair kept prices with their observations for receipts + recency
	keptCount := map[string]int{}
	for _, p := range kept {
		keptCount[p.String()]++
	}
	var trimmed []Observation
	for _, o := range inCell {
		if keptCount[o.PriceUSD.String()] > 0 {
			keptCount[o.PriceUSD.String()]--
			trimmed = append(trimmed, o)
		}
	}

	sorted := make([]money.Decimal, len(trimmed))
	for i, o := range trimmed {
		sorted[i] = o.PriceUSD
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].LessThan(sorted[j]) })
	n := len(sorted)

	median := weightedMedian(trimmed, now)
	p25 := sorted[(n-1)/4]
	p75 := sorted[3*(n-1)/4]
	p10 := sorted[max(0, n/10)]
	p90 := sorted[min(n-1, (9*n)/10)]

	confidence := "low"
	if n >= Current.HighConfidence {
		confidence = "high"
	} else if n >= Current.MediumConfidence {
		confidence = "medium"
	}

	// receipts: most recent, max N
	recent := make([]Observation, len(trimmed))
	copy(recent, trimmed)
	sort.Slice(recent, func(i, j int) bool { return recent[i].ObservedAt.After(recent[j].ObservedAt) })
	receipts := make([]Receipt, 0, Current.MaxReceipts)
	for i, o := range recent {
		if i >= Current.MaxReceipts {
			break
		}
		receipts = append(receipts, Receipt{
			Source:   o.Source,
			Title:    o.Title,
			URL:      o.URL,
			Ref:      o.Ref,
			Date:     o.ObservedAt.Format("2006-01"),
			PriceUSD: o.PriceUSD.String(),
		})
	}

	failing := EvaluateGates(inCell, p25, p75, now)
	status := "pass"
	if len(failing) > 0 {
		status = "limited"
	}

	return &Verdict{
		CellKey:      key,
		Brand:        brand,
		Model:        model,
		Dial:         dial,
		Material:     material,
		Scope:        scope,
		P10:          p10,
		Median:       median,
		P90:          p90,
		Count:        n,
		Confidence:   confidence,
		ComputedAt:   now,
		Receipts:     receipts,
		GatesStatus:  status,
		FailingGates: failing,
		RulesetHash:  Current.Hash(),
		InputsHash:   InputsHash(key, inCell),
	}
}

// weightedMedian — recency-weighted; weights are floats (not money).
func weightedMedian(obs []Observation, now time.Time) money.Decimal {
	type pair struct {
		price  money.Decimal
		weight float64
	}
	pairs := make([]pair, 0, len(obs))
	var total float64
	for _, o := range obs {
		ageDays := now.Sub(o.ObservedAt).Hours() / 24.0
		w := math.Pow(0.5, ageDays/Current.HalfLifeDays)
		pairs = append(pairs, pair{o.PriceUSD, w})
		total += w
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].price.LessThan(pairs[j].price) })
	if total <= 0 {
		return obs[len(obs)/2].PriceUSD
	}
	var cum float64
	for _, p := range pairs {
		cum += p.weight
		if cum >= total/2 {
			return p.price
		}
	}
	return pairs[len(pairs)-1].price
}

// trimTypicalBand — dominant-cluster trim (PLAN: never the mean; the band is
// the honest answer). Prices outside [median*low, median*high] drop, up to N
// iterations or until stable.
func trimTypicalBand(prices []money.Decimal) []money.Decimal {
	if len(prices) < 8 {
		return prices
	}
	kept := make([]money.Decimal, len(prices))
	copy(kept, prices)
	for i := 0; i < Current.TrimIterations; i++ {
		median := kept[len(kept)/2]
		lo := median.Mul(money.MustDecimal(strconv.FormatFloat(Current.TrimLowMult, 'f', -1, 64)))
		hi := median.Mul(money.MustDecimal(strconv.FormatFloat(Current.TrimHighMult, 'f', -1, 64)))
		var next []money.Decimal
		for _, p := range kept {
			if p.GreaterThanOrEqual(lo) && p.LessThanOrEqual(hi) {
				next = append(next, p)
			}
		}
		if len(next) < 8 || sameDecimals(next, kept) {
			kept = next
			break
		}
		kept = next
	}
	return kept
}

func sameDecimals(a, b []money.Decimal) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
