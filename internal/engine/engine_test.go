package engine

import (
	"testing"
	"time"
)

func mkobs(price float64, ageDays float64) Observation {
	return Observation{
		Brand: "Rolex", Model: "GMT-Master II", Dial: "batman",
		Material: "steel", Scope: "full_set",
		PriceUSD: price, ObservedAt: time.Now().Add(-time.Duration(ageDays * 24 * float64(time.Hour))),
	}
}

func TestCellKeyIncludesModel(t *testing.T) {
	// regression: the cell key once omitted the model — a GMT and a Datejust
	// would have shared a cell
	a := CellKey("Rolex", "GMT-Master II", "black", "steel", "full_set")
	b := CellKey("Rolex", "Datejust", "black", "steel", "full_set")
	if a == b {
		t.Fatal("different models must never share a cell")
	}
}

func TestUnknownOnlyMatchesUnknown(t *testing.T) {
	stated := CellKey("Rolex", "GMT-Master II", "batman", "steel", "full_set")
	unstated := CellKey("Rolex", "GMT-Master II", "", "steel", "full_set")
	if stated == unstated {
		t.Fatal("stated dial must not share a cell with unknown dial")
	}
	// but unknown matches unknown
	a := CellKey("Rolex", "GMT-Master II", "", "steel", "")
	b := CellKey("Rolex", "GMT-Master II", "", "steel", "")
	if a != b {
		t.Fatal("identical unknowns must share a cell")
	}
}

func TestBelowMinimumSamplesRefuses(t *testing.T) {
	thin := []Observation{mkobs(10000, 1), mkobs(11000, 2), mkobs(12000, 3)}
	if v := ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set", thin, time.Now()); v != nil {
		t.Fatal("must refuse below 4 samples")
	}
}

func testCellObservations() []Observation {
	// 20 observations, tight cluster with two outliers
	prices := []float64{10000, 10200, 10500, 10800, 11000, 11200, 11500, 11800,
		12000, 12200, 12500, 12800, 13000, 13200, 13500, 13800, 14000, 14200,
		45000, 100}
	obs := make([]Observation, len(prices))
	for i, p := range prices {
		obs[i] = mkobs(p, float64(i+1))
	}
	return obs
}

func testCellVerdict() *Verdict {
	return ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set",
		testCellObservations(), time.Now())
}

func testCellVerdictAt(now time.Time) *Verdict {
	return ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set",
		testCellObservations(), now)
}

func testCellVerdictTrim() *Verdict {
	return ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set",
		testCellObservations(), time.Now())
}

func TestVerdictComputedForSufficientCell(t *testing.T) {
	v := testCellVerdictAt(time.Now())
	if v == nil {
		t.Fatal("20 observations must produce a verdict")
	}
	if v.Count != 18 {
		t.Errorf("count = %d, want 18 (outliers trimmed)", v.Count)
	}
	if v.Median < 10000 || v.Median > 15000 {
		t.Errorf("median %.0f outside sane cluster", v.Median)
	}
	if v.P25 > v.Median || v.Median > v.P75 {
		t.Errorf("band ordering broken: p25=%.0f median=%.0f p75=%.0f", v.P25, v.Median, v.P75)
	}
	// 18 trimmed comps = medium (high requires 20 untrimmed)
	if v.Confidence != "medium" {
		t.Errorf("18 comps after trim should be medium confidence, got %q", v.Confidence)
	}
	if len(v.Receipts) == 0 || len(v.Receipts) > Current.MaxReceipts {
		t.Errorf("receipts = %d, want 1..%d", len(v.Receipts), Current.MaxReceipts)
	}
}

func testVerdictTrimmed() *Verdict {
	return ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set",
		testCellObservations(), time.Now())
}

func TestOutliersExcludedFromBand(t *testing.T) {
	v := testVerdictTrimmed()
	// $100 and $45,000 are outliers — the fair band must exclude them
	if v.FairLow < 5000 {
		t.Errorf("fair low %.0f too low — outlier leaked into band", v.FairLow)
	}
	if v.FairHigh > 20000 {
		t.Errorf("fair high %.0f too high — outlier leaked into band", v.FairHigh)
	}
}

func TestRecencyWeightingShiftsMedian(t *testing.T) {
	// 10 old sales at 10k, then 10 recent sales at 14k:
	// recency-weighted median must land nearer 14k than a plain median would
	now := time.Now()
	obs := []Observation{}
	for i := 0; i < 10; i++ {
		o := mkobs(10000, 0)
		o.ObservedAt = now.Add(-300 * 24 * time.Hour) // old
		obs = append(obs, o)
	}
	for i := 0; i < 10; i++ {
		o := mkobs(14000, 0)
		o.ObservedAt = now.Add(-5 * 24 * time.Hour) // recent
		obs = append(obs, o)
	}
	v := ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set", obs, now)
	if v == nil {
		t.Fatal("expected verdict")
	}
	if v.Median <= 10000 {
		t.Errorf("recency weighting failed: median %.0f should be pulled toward recent 14k", v.Median)
	}
}

func TestUnknownDialIsSeparateCell(t *testing.T) {
	obs := testCellObservations()
	for i := range obs {
		obs[i].Dial = "" // unstated
	}
	v := ComputeVerdict("Rolex", "GMT-Master II", "batman", "steel", "full_set", obs, time.Now())
	if v != nil {
		t.Fatal("batman query must not be answered by unknown-dial listings")
	}
}
