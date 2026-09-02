// Command gengolden regenerates internal/engine/golden.json — the frozen
// observation sets and their frozen verdicts (PLAN.md §13 golden-verdict suite).
//
// Run ONLY when a ruleset version is deliberately changed and re-blessed:
//
//	go run ./cmd/gengolden && git diff internal/engine/golden.json
//
// An unintended diff here is a silent shift in pricing logic — stop, investigate.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/money"
)

var fixedNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type obsSpec struct {
	Source  string `json:"source"`
	Title   string `json:"title"`
	Price   string `json:"price_usd"`
	DaysAgo int    `json:"days_ago"`
}

type scenario struct {
	name     string
	brand    string
	model    string
	dial     string
	material string
	scope    string
	specs    []obsSpec
}

func main() {
	sub := func(base string, i int) string {
		return money.MustDecimal(base).Add(money.MustDecimal(fmt.Sprint(i * 100))).String()
	}

	scenarios := []scenario{
		{
			name: "pass_all_gates", brand: "Rolex", model: "Submariner Date", dial: "black", material: "steel",
			specs: []obsSpec{
				{"chrono24", "Submariner Date 126610LN", "13800", 30}, {"chrono24", "126610LN box", "14200", 60},
				{"bobswatches", "Submariner Date black", "14000", 45}, {"bobswatches", "126610LN", "14500", 90},
				{"auction_a", "Submariner Date realised", "12900", 15}, {"auction_b", "126610LN realised", "13100", 40},
				{"chrono24", "126610LN full set", "15100", 75}, {"bobswatches", "Submariner Date", "14400", 120},
				{"watchfinder", "126610LN steel black", "13900", 10}, {"auction_c", "Submariner LN", "13400", 55},
				{"chrono24", "Rolex Sub LN", "14100", 5}, {"bobswatches", "Submariner Date black", "14600", 35},
			},
		},
		{
			name: "fail_volume", brand: "Rolex", model: "Submariner Date", dial: "green", material: "steel",
			specs: []obsSpec{
				{"chrono24", "126610LV hulk", "17800", 30}, {"bobswatches", "126610LV green", "18200", 60},
				{"auction_a", "126610LV realised", "17100", 20}, {"watchfinder", "Hulk 126610LV", "17500", 10},
				{"chrono24", "126610LV", "18000", 75}, {"bobswatches", "Submariner LV", "18400", 120},
			},
		},
		{
			name: "fail_sources", brand: "Rolex", model: "Submariner Date", dial: "black", material: "steel", scope: "full_set",
			specs: twelveFromOneSource(),
		},
		{
			name: "fail_freshness", brand: "Rolex", model: "Submariner Date", dial: "black", material: "steel", scope: "naked",
			specs: []obsSpec{
				{"chrono24", "126610LN stale", "13800", 200}, {"chrono24", "126610LN stale2", "13900", 210},
				{"chrono24", "126610LN stale3", "14000", 220}, {"bobswatches", "126610LN stale4", "14100", 230},
				{"chrono24", "126610LN stale5", "14200", 240}, {"bobswatches", "126610LN stale6", "14300", 250},
				{"watchfinder", "126610LN stale7", "14400", 260}, {"chrono24", "126610LN stale8", "14500", 270},
				{"bobswatches", "126610LN stale9", "14600", 280}, {"watchfinder", "126610LN stale10", "14700", 290},
			},
		},
		{
			name: "fail_dispersion", brand: "Rolex", model: "GMT-Master II", dial: "pepsi", material: "steel",
			specs: []obsSpec{
				{"chrono24", "GMT 126710BLRO", "18000", 30}, {"bobswatches", "126710BLRO pepsi", "18400", 60},
				{"auction_a", "BLRO realised", "17100", 20}, {"watchfinder", "Pepsi 126710BLRO", "18200", 10},
				{"chrono24", "126710BLRO", "18600", 75}, {"bobswatches", "GMT pepsi steel", "18300", 90},
				{"watchfinder", "Rolex GMT BLRO", "18700", 40}, {"chrono24", "BLRO 126710", "18500", 15},
				{"auction_b", "GMT BLRO realised", "17300", 55}, {"bobswatches", "126710BLRO", "18650", 25},
				{"chrono24", "Rolex GMT-Master II fantasy", "99000", 12}, {"watchfinder", "126710BLRO pepsi", "18550", 33},
			},
		},
		{
			name: "thin_no_verdict", brand: "Rolex", model: "Milgauss", dial: "white", material: "steel",
			specs: []obsSpec{
				{"chrono24", "Milgauss 116400 white", "6800", 30},
				{"bobswatches", "Milgauss white", "7100", 60},
				{"watchfinder", "Rolex Milgauss", "6900", 20},
			},
		},
	}

	type outObs = obsSpec
	type outScenario struct {
		Name         string   `json:"name"`
		Brand        string   `json:"brand"`
		Model        string   `json:"model"`
		Dial         string   `json:"dial"`
		Material     string   `json:"material"`
		Scope        string   `json:"scope"`
		Observations []outObs `json:"observations"`
		Expected     *struct {
			Count        int      `json:"count"`
			Median       string   `json:"median"`
			P10          string   `json:"p10"`
			P90          string   `json:"p90"`
			GatesStatus  string   `json:"gates_status"`
			FailingGates []string `json:"failing_gates"`
		} `json:"expected"`
	}
	_ = sub

	type golden struct {
		Ruleset   engine.Ruleset `json:"ruleset"`
		FixedNow  string         `json:"fixed_now"`
		Scenarios []outScenario  `json:"scenarios"`
	}

	g := golden{Ruleset: engine.Current, FixedNow: fixedNow.Format(time.RFC3339)}
	for _, sc := range scenarios {
		var obs []engine.Observation
		for i, sp := range sc.specs {
			obs = append(obs, engine.Observation{
				Brand: sc.brand, Model: sc.model, Dial: sc.dial, Material: sc.material, Scope: sc.scope,
				Source:     sp.Source,
				Title:      sp.Title,
				URL:        fmt.Sprintf("https://example.com/%s/%d", sp.Source, i),
				PriceUSD:   money.MustDecimal(sp.Price),
				ObservedAt: fixedNow.Add(-time.Duration(sp.DaysAgo) * 24 * time.Hour),
			})
		}
		v := engine.ComputeVerdict(sc.brand, sc.model, sc.dial, sc.material, sc.scope, obs, fixedNow)
		outSc := outScenario{
			Name: sc.name, Brand: sc.brand, Model: sc.model, Dial: sc.dial, Material: sc.material, Scope: sc.scope,
			Observations: sc.specs,
		}
		if v != nil {
			outSc.Expected = &struct {
				Count        int      `json:"count"`
				Median       string   `json:"median"`
				P10          string   `json:"p10"`
				P90          string   `json:"p90"`
				GatesStatus  string   `json:"gates_status"`
				FailingGates []string `json:"failing_gates"`
			}{
				Count: v.Count, Median: v.Median.String(), P10: v.P10.String(), P90: v.P90.String(),
				GatesStatus: v.GatesStatus, FailingGates: v.FailingGates,
			}
		}
		g.Scenarios = append(g.Scenarios, outSc)
	}

	b, _ := json.MarshalIndent(g, "", "  ")
	if err := os.WriteFile("internal/engine/golden.json", b, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("golden.json written: %d scenarios, ruleset %s (%.12s)\n", len(scenarios), g.Ruleset.Version, g.Ruleset.Hash())
}

func twelveFromOneSource() []obsSpec {
	var s []obsSpec
	for i := 0; i < 12; i++ {
		s = append(s, obsSpec{"chrono24", fmt.Sprintf("126610LN full set #%d", i), fmt.Sprintf("%d", 13800+i*100), 30 + i})
	}
	return s
}
