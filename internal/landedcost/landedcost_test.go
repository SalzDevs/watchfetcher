package landedcost

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"watchledger/internal/money"
)

var fxFixture = FX{
	Date: "2026-09-01",
	Rates: map[string]money.Decimal{
		"JPY": money.MustDecimal("157.0"),
		"USD": money.MustDecimal("1.09"),
		"GBP": money.MustDecimal("0.85"),
		"CHF": money.MustDecimal("0.94"),
		"HKD": money.MustDecimal("8.51"),
		"EUR": money.MustDecimal("1.0"),
	},
}

func rule(from, to, duty, vat, vatBasis string) Rule {
	return Rule{
		FromCountry: from, ToCountry: to, HsCode: "9102.21",
		DutyRate: money.MustDecimal(duty), VatRate: money.MustDecimal(vat),
		VatBasis: vatBasis, InsurancePct: money.MustDecimal("0.5"),
		Basis: "test basis", SourceURL: "https://example.com/tariff",
		VerifiedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestGoldenLandedCost — frozen corridors → frozen breakdowns.
// Regenerate with: GEN_GOLDEN=1 go test ./internal/landedcost/
func TestGoldenLandedCost(t *testing.T) {
	type goldenLine struct {
		Label  string `json:"label"`
		Amount string `json:"amount"`
	}
	type goldenCase struct {
		Name     string       `json:"name"`
		Result   []goldenLine `json:"lines"`
		Total    string       `json:"total"`
		Warnings int          `json:"warnings"`
	}

	cases := []struct {
		name string
		rule Rule
		in   Input
	}{
		{
			name: "jp-pt",
			rule: rule("JP", "PT", "4.5", "23", "cif_plus_duty"),
			in: Input{
				ItemPrice: money.MustDecimal("3200000"), Currency: "JPY",
				FromCountry: "JP", ToCountry: "PT",
				Shipping: money.MustDecimal("180"),
			},
		},
		{
			name: "ch-de",
			rule: rule("CH", "DE", "4.5", "19", "cif_plus_duty"),
			in: Input{
				ItemPrice: money.MustDecimal("18500"), Currency: "CHF",
				FromCountry: "CH", ToCountry: "DE",
				Shipping: money.MustDecimal("150"),
			},
		},
		{
			name: "us-uk-no-vat",
			rule: rule("US", "UK", "0", "20", "cif_plus_duty"),
			in: Input{
				ItemPrice: money.MustDecimal("14000"), Currency: "USD",
				FromCountry: "US", ToCountry: "UK",
				Shipping: money.MustDecimal("120"),
			},
		},
		{
			name: "jp-us-no-vat",
			rule: rule("JP", "US", "5", "0", "none"),
			in: Input{
				ItemPrice: money.MustDecimal("3200000"), Currency: "JPY",
				FromCountry: "JP", ToCountry: "US",
				Shipping: money.MustDecimal("200"),
			},
		},
	}

	update := os.Getenv("GEN_GOLDEN") == "1"
	path := "golden_landedcost.json"
	var file struct {
		FXDate string       `json:"fx_date"`
		Cases  []goldenCase `json:"cases"`
	}

	if update {
		for _, c := range cases {
			res, err := Compute(c.rule, fxFixture, c.in)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			gl := goldenCase{Name: c.name, Total: res.Total.StringFixed(2), Warnings: len(res.Warnings)}
			for _, l := range res.Lines {
				gl.Result = append(gl.Result, goldenLine{Label: l.Label, Amount: l.Amount.StringFixed(2)})
			}
			file.Cases = append(file.Cases, gl)
		}
		file.FXDate = fxFixture.Date
		b, _ := json.MarshalIndent(file, "", "  ")
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden_landedcost.json missing — run GEN_GOLDEN=1 go test ./internal/landedcost/ once: %v", err)
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.FXDate != fxFixture.Date {
		t.Fatalf("fx fixture drift: golden=%s test=%s", file.FXDate, fxFixture.Date)
	}
	if len(file.Cases) != len(cases) {
		t.Fatalf("scenario count drift: golden=%d test=%d", len(file.Cases), len(cases))
	}
	for i, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			res, err := Compute(c.rule, fxFixture, c.in)
			if err != nil {
				t.Fatal(err)
			}
			want := file.Cases[i]
			if len(res.Lines) != len(want.Result) {
				t.Fatalf("line count: want %d got %d", len(want.Result), len(res.Lines))
			}
			for j, l := range res.Lines {
				w := want.Result[j]
				if l.Amount.StringFixed(2) != w.Amount {
					t.Errorf("line %q: want %s got %s", l.Label, w.Amount, l.Amount.StringFixed(2))
				}
			}
			if res.Total.StringFixed(2) != want.Total {
				t.Errorf("total: want %s got %s", want.Total, res.Total.StringFixed(2))
			}
		})
	}
}

// TestMarginSchemeWarns — the assumption is surfaced, never silently applied.
func TestMarginSchemeWarns(t *testing.T) {
	r := rule("JP", "PT", "4.5", "23", "cif_plus_duty")
	in := Input{ItemPrice: money.MustDecimal("3200000"), Currency: "JPY", FromCountry: "JP", ToCountry: "PT", Shipping: money.MustDecimal("180"), MarginScheme: true}
	res, err := Compute(r, fxFixture, in)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if len(w) > 12 && strings.HasPrefix(w, "margin scheme") {
			found = true
		}
	}
	if !found {
		t.Fatal("margin scheme warning missing")
	}
}

// TestStaleRuleFlags — 180-day wall renders stale on duty/vat lines.
func TestStaleRuleFlags(t *testing.T) {
	r := rule("JP", "PT", "4.5", "23", "cif_plus_duty")
	r.VerifiedAt = time.Now().Add(-200 * 24 * time.Hour)
	in := Input{ItemPrice: money.MustDecimal("3200000"), Currency: "JPY", FromCountry: "JP", ToCountry: "PT", Shipping: money.MustDecimal("180")}
	res, err := Compute(r, fxFixture, in)
	if err != nil {
		t.Fatal(err)
	}
	staleCount := 0
	for _, l := range res.Lines {
		if l.Stale {
			staleCount++
		}
	}
	if staleCount < 2 {
		t.Fatalf("duty+vat lines must be flagged stale, got %d", staleCount)
	}
}

// TestAmbiguousCorridorFails — no rule, no number (G9).
func TestMissingRuleFails(t *testing.T) {
	r := rule("JP", "PT", "4.5", "23", "cif_plus_duty")
	in := Input{ItemPrice: money.MustDecimal("100"), Currency: "JPY", FromCountry: "JP", ToCountry: "BR"}
	if _, err := Compute(r, fxFixture, in); err == nil {
		t.Fatal("corridor mismatch must fail")
	}
}
