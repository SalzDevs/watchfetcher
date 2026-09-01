// Command gengolden regenerates internal/engine/golden.json — the frozen
// observation sets and their frozen verdicts (PLAN.md §13 golden-verdict suite).
// Run ONLY when a ruleset version is deliberately changed and re-blessed:
//
//	go run ./cmd/gengolden && git diff internal/engine/golden.json
//
// An unintended diff here is a silent shift in pricing logic — stop and investigate.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"watchfetcher/internal/engine"
)

var fixedNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

type obsSpec struct {
	Source string
	Title  string
	Price  float64
	DaysAgo int
}

func build(cell [5]string, specs []obsSpec) (string, []engine.Observation) {
	obs := make([]engine.Observation, 0, len(specs))
	for i, sp := range specs {
		obs = append(obs, engine.Observation{
			Brand: cell[0], Model: cell[1], Dial: cell[2], Material: cell[3], Scope: cell[4],
			Source: sp.Source, Title: sp.Title, URL: fmt.Sprintf("https://example.com/%s/%d", sp.Source, i),
			PriceUSD: sp.Price, ObservedAt: fixedNow.Add(-time.Duration(sp.DaysAgo) * 24 * time.Hour),
		})
	}
	return engine.CellKey(cell[0], cell[1], cell[2], cell[3], cell[4]), obs
}

func main() {
	type scenario struct {
		Name       string           `json:"name"`
		CellKey    string           `json:"cell_key"`
		Verdict    *engine.Verdict  `json:"verdict"`
	}
	var scenarios []scenario

	add := func(name string, cell [5]string, specs []obsSpec) {
		key, obs := build(cell, specs)
		v := engine.ComputeVerdict(cell[0], cell[1], cell[2], cell[3], cell[4], obs, fixedNow)
		scenarios = append(scenarios, scenario{Name: name, CellKey: key, Verdict: v})
	}

	// 1. healthy cell: 12 comps, 3 sources, fresh, tight — all gates pass
	add("pass_all_gates", [5]string{"Rolex", "Submariner Date", "black", "steel", ""},
		[]obsSpec{
			{"chrono24", "Rolex Submariner Date 126610LN", 13800, 30}, {"chrono24", "Rolex Submariner 126610LN box", 14200, 60},
			{"bobswatches", "Rolex Submariner Date black", 14000, 45}, {"bobswatches", "Rolex 126610LN", 14500, 90},
			{"the1916company", "Submariner Date 126610LN", 14900, 20}, {"watchfinder", "Rolex Submariner black", 13600, 10},
			{"chrono24", "Submariner 126610LN full set", 15100, 75}, {"bobswatches", "Rolex Submariner Date", 14400, 120},
			{"watchfinder", "126610LN steel black", 13900, 15}, {"the1916company", "Submariner Date", 14750, 55},
			{"chrono24", "Rolex Sub LN 126610", 14100, 5}, {"bobswatches", "Submariner Date black 126610LN", 14600, 35},
		})

	// 2. volume gate fail: only 6 comps
	add("fail_volume", [5]string{"Rolex", "Submariner Date", "green", "steel", ""},
		[]obsSpec{
			{"chrono24", "Rolex Submariner 126610LV Hulk", 17800, 30}, {"bobswatches", "Submariner green 126610LV", 18200, 60},
			{"the1916company", "126610LV green", 18500, 20}, {"watchfinder", "Rolex Hulk 126610LV", 17500, 10},
			{"chrono24", "126610LV", 18000, 75}, {"bobswatches", "Submariner Date LV", 18400, 120},
		})

	// 3. sources gate fail: 12 comps, single source
	specs3 := []obsSpec{}
	for i := 0; i < 12; i++ {
		specs3 = append(specs3, obsSpec{"chrono24", fmt.Sprintf("Rolex Submariner 126610LN #%d", i), 13800 + float64(i*100), 30 + i})
	}
	add("fail_sources", [5]string{"Rolex", "Submariner Date", "black", "steel", "full_set"}, specs3)

	// 4. freshness gate fail: comps all older than 180 days
	specs4 := []obsSpec{}
	for i := 0; i < 10; i++ {
		specs4 = append(specs4, obsSpec{"chrono24", "Rolex 126610LN stale", 13800 + float64(i*50), 200 + i*10})
	}
	specs4[3].Source = "bobswatches"
	specs4[6].Source = "watchfinder"
	add("fail_freshness", [5]string{"Rolex", "Submariner Date", "black", "steel", "naked"}, specs4)

	// 5. dispersion gate fail: one fantasy ask blows the band
	add("fail_dispersion", [5]string{"Rolex", "GMT-Master II", "pepsi", "steel", ""},
		[]obsSpec{
			{"chrono24", "GMT 126710BLRO", 18000, 30}, {"bobswatches", "126710BLRO pepsi", 18400, 60},
			{"the1916company", "GMT-Master II BLRO", 18800, 20}, {"watchfinder", "Pepsi 126710BLRO", 18200, 10},
			{"chrono24", "126710BLRO", 18600, 75}, {"bobswatches", "GMT pepsi steel", 18300, 90},
			{"watchfinder", "Rolex GMT 126710BLRO", 18700, 40}, {"chrono24", "BLRO 126710", 18500, 15},
			{"the1916company", "GMT Master II pepsi", 18450, 50}, {"bobswatches", "126710BLRO", 18650, 25},
			{"chrono24", "Rolex GMT-Master II", 99000, 12}, // fantasy ask
			{"watchfinder", "126710BLRO pepsi bezel", 18550, 33},
		})

	// 6. below MinSamples entirely — no verdict at all
	add("no_verdict_thin", [5]string{"Rolex", "Milgauss", "white", "steel", ""},
		[]obsSpec{
			{"chrono24", "Milgauss 116400 white", 6800, 30},
			{"bobswatches", "Milgauss white gv", 7100, 60},
			{"watchfinder", "Rolex Milgauss", 6900, 20},
		})

	out := struct {
		Ruleset   engine.Ruleset `json:"ruleset"`
		FixedNow  string         `json:"fixed_now"`
		Scenarios []scenario     `json:"scenarios"`
	}{engine.Current, fixedNow.Format(time.RFC3339), scenarios}

	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile("internal/engine/golden.json", b, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("golden.json written: %d scenarios, ruleset %s\n", len(scenarios), out.Ruleset.Version)
}
