package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"watchledger/internal/money"
)

var fixedNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func usd(s string) money.Decimal { return money.MustDecimal(s) }

// goldenFile is the committed fixture: frozen ruleset + frozen observation
// sets + their frozen verdicts. Byte-stable unless a ruleset change is
// deliberate (PLAN.md §13).
type goldenFile struct {
	Ruleset   Ruleset `json:"ruleset"`
	FixedNow  string  `json:"fixed_now"`
	Scenarios []struct {
		Name         string       `json:"name"`
		Brand        string       `json:"brand"`
		Model        string       `json:"model"`
		Dial         string       `json:"dial"`
		Material     string       `json:"material"`
		Scope        string       `json:"scope"`
		Observations []GoldenObs  `json:"observations"`
		Expected     *VerdictCore `json:"expected"`
	} `json:"scenarios"`
}

// GoldenObs is one frozen observation (full fidelity — day precision).
type GoldenObs struct {
	Source   string `json:"source"`
	Title    string `json:"title"`
	PriceUSD string `json:"price_usd"`
	DaysAgo  int    `json:"days_ago"`
}

// VerdictCore is the published subset asserted by the suite.
type VerdictCore struct {
	Count        int      `json:"count"`
	Median       string   `json:"median"`
	P10          string   `json:"p10"`
	P90          string   `json:"p90"`
	GatesStatus  string   `json:"gates_status"`
	FailingGates []string `json:"failing_gates"`
}

func (f *goldenFile) obs(sc int) ([]Observation, int) {
	s := f.Scenarios[sc]
	out := make([]Observation, 0, len(s.Observations))
	for i, o := range s.Observations {
		out = append(out, Observation{
			Brand: s.Brand, Model: s.Model, Dial: s.Dial, Material: s.Material, Scope: s.Scope,
			Source: o.Source, Title: o.Title,
			URL:        fmt.Sprintf("https://example.com/%s/%d", o.Source, i),
			PriceUSD:   usd(o.PriceUSD),
			ObservedAt: fixedNow.Add(-time.Duration(o.DaysAgo) * 24 * time.Hour),
		})
	}
	return out, sc
}

// TestGoldenSuite — recompute every scenario, compare to frozen expectations.
// Regenerate ONLY with a deliberate ruleset version bump:
//
//	go run ./cmd/gengolden && git diff internal/engine/golden.json
func TestGoldenSuite(t *testing.T) {
	raw, err := os.ReadFile("golden.json")
	if err != nil {
		t.Fatalf("golden.json missing: %v", err)
	}
	var file goldenFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse golden.json: %v", err)
	}
	if file.Ruleset.Hash() != Current.Hash() {
		t.Fatalf("golden.json blessed for ruleset %s (%.12s) but Current hash is %.12s — deliberate change? bump version + regenerate",
			file.Ruleset.Version, file.Ruleset.Hash(), Current.Hash())
	}
	if file.FixedNow != fixedNow.Format(time.RFC3339) {
		t.Fatalf("fixed_now mismatch: golden=%s test=%s", file.FixedNow, fixedNow.Format(time.RFC3339))
	}
	if len(file.Scenarios) == 0 {
		t.Fatal("empty golden scenarios")
	}
	for i := range file.Scenarios {
		sc := &file.Scenarios[i]
		t.Run(sc.Name, func(t *testing.T) {
			obs, _ := file.obs(i)
			got := ComputeVerdict(sc.Brand, sc.Model, sc.Dial, sc.Material, sc.Scope, obs, fixedNow)
			want := sc.Expected
			if want == nil {
				if got != nil {
					t.Fatalf("expected no verdict, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected verdict, got nil")
			}
			if got.Count != want.Count {
				t.Errorf("count: want %d got %d", want.Count, got.Count)
			}
			if !got.Median.Equal(usd(want.Median)) {
				t.Errorf("median: want %s got %s", want.Median, got.Median)
			}
			if !got.P10.Equal(usd(want.P10)) {
				t.Errorf("p10: want %s got %s", want.P10, got.P10)
			}
			if !got.P90.Equal(usd(want.P90)) {
				t.Errorf("p90: want %s got %s", want.P90, got.P90)
			}
			if got.GatesStatus != want.GatesStatus {
				t.Errorf("gates_status: want %s got %s (failing %v)", want.GatesStatus, got.GatesStatus, got.FailingGates)
			}
			if len(got.FailingGates) != len(want.FailingGates) {
				t.Errorf("failing_gates: want %v got %v", want.FailingGates, got.FailingGates)
			}
			// determinism: identical inputs → identical verdict bytes
			b1, _ := json.Marshal(got)
			again := ComputeVerdict(sc.Brand, sc.Model, sc.Dial, sc.Material, sc.Scope, obs, fixedNow)
			b2, _ := json.Marshal(again)
			if string(b1) != string(b2) {
				t.Error("verdict not deterministic")
			}
		})
	}
}

// TestInputsHashOrderIndependent — the ledger never promises row order.
func TestInputsHashOrderIndependent(t *testing.T) {
	a := []Observation{
		{Source: "s1", Title: "a", PriceUSD: usd("100"), ObservedAt: fixedNow},
		{Source: "s2", Title: "b", PriceUSD: usd("200"), ObservedAt: fixedNow.Add(time.Hour)},
	}
	b := []Observation{a[1], a[0]}
	if InputsHash("cell", a) != InputsHash("cell", b) {
		t.Fatal("inputs hash must be order-independent")
	}
	a[1].PriceUSD = usd("201")
	if InputsHash("cell", a) == InputsHash("cell", b) {
		t.Fatal("changed evidence must change the hash")
	}
}

// TestGatesShape — each gate fails alone when it should.
func TestGatesShape(t *testing.T) {
	mk := func(i int) Observation {
		return Observation{
			Brand: "Rolex", Model: "Sub", Source: fmt.Sprintf("s%d", i%3),
			Title:      fmt.Sprintf("t%d", i),
			PriceUSD:   usd("1000"),
			ObservedAt: fixedNow.Add(-time.Duration(10+i) * 24 * time.Hour),
		}
	}
	var obs []Observation
	for i := 0; i < 7; i++ {
		obs = append(obs, mk(i))
	}
	failing := EvaluateGates(obs, usd("900"), usd("1100"), fixedNow)
	if len(failing) != 1 || failing[0] != "volume" {
		t.Fatalf("want [volume], got %v", failing)
	}
	obs = append(obs, mk(7))
	if failing := EvaluateGates(obs, usd("900"), usd("1100"), fixedNow); len(failing) != 0 {
		t.Fatalf("want [], got %v", failing)
	}
}
