// Package landedcost: the zero-data product (PLAN.md §8). Pure, deterministic,
// Decimal-only. Every line item carries its basis; assumptions surface as
// warnings; output pins fx_date + ruleset_version so any result re-derives.
package landedcost

import (
	"fmt"
	"time"

	"watchledger/internal/money"
)

// MaxRuleAgeDays — the staleness wall (PLAN.md §8 rule 4).
const MaxRuleAgeDays = 180

// CardSpreadPct — stated FX spread, its own line, never baked into a rate.
const CardSpreadPct = "1.5"

// Rule is one corridor's duty/VAT configuration (DB row, verified).
type Rule struct {
	FromCountry    string
	ToCountry      string
	HsCode         string
	DutyRate       money.Decimal // percent
	VatRate        money.Decimal // percent
	VatBasis       string        // cif_plus_duty | none
	InsurancePct   money.Decimal // percent of CIF
	Basis          string
	SourceURL      string
	VerifiedAt     time.Time
	VerifiedBy     string
	RulesetVersion string
}

// FX — reference rates for one date, units of currency per EUR.
type FX struct {
	Date  string                   `json:"date"` // YYYY-MM-DD
	Rates map[string]money.Decimal `json:"rates"`
}

func (f FX) Rate(ccy string) (money.Decimal, error) {
	r, ok := f.Rates[ccy]
	if !ok {
		return money.Zero, fmt.Errorf("no FX rate for %s on %s", ccy, f.Date)
	}
	if !r.IsPositive() {
		return money.Zero, fmt.Errorf("bad FX rate for %s", ccy)
	}
	return r, nil
}

// Input — user-facing assumptions, all editable, none silently applied (§8 rule 3).
type Input struct {
	ItemPrice    money.Decimal // in Currency
	Currency     string        // source currency
	FromCountry  string
	ToCountry    string
	Shipping     money.Decimal // in destination currency
	InsurancePct money.Decimal // percent; 0 → rule default
	MarginScheme bool          // private-sale toggle
	AskPrice     money.Decimal // original ask (optional, for the over-ask line)
}

// Line is one breakdown row. Amount is in destination currency, quantized 2dp.
type Line struct {
	Label  string        `json:"label"`
	Amount money.Decimal `json:"amount"`
	Basis  string        `json:"basis"`
	Stale  bool          `json:"stale,omitempty"`
}

// Result — the breakdown. Never a single number without the lines (PLAN §8).
type Result struct {
	Corridor       string        `json:"corridor"` // 'JP-PT'
	DestCurrency   string        `json:"dest_currency"`
	Lines          []Line        `json:"lines"`
	Total          money.Decimal `json:"total"`
	OverAskPct     money.Decimal `json:"over_ask_pct,omitempty"` // percent vs ask, when ask given
	FXDate         string        `json:"fx_date"`
	RulesetVersion string        `json:"ruleset_version"`
	Warnings       []string      `json:"warnings,omitempty"`
}

// CurrencyFor — static, boring mapping. Countries we ship corridors for.
func CurrencyFor(country string) (string, error) {
	switch country {
	case "JP":
		return "JPY", nil
	case "US":
		return "USD", nil
	case "GB", "UK":
		return "GBP", nil
	case "CH":
		return "CHF", nil
	case "HK":
		return "HKD", nil
	case "PT", "DE", "FR", "IT", "ES", "NL", "AT", "BE", "IE":
		return "EUR", nil
	}
	return "", fmt.Errorf("unsupported country %q", country)
}

// twoDP quantizes for display/storage — once, at line construction (PLAN §8 rule 1).
func twoDP(d money.Decimal) money.Decimal {
	return d.Round(2)
}

// Compute — pure. rule + fx are loaded by the caller; this function never
// touches a database or the network (G1).
func Compute(rule Rule, fx FX, in Input) (Result, error) {
	res := Result{Corridor: rule.FromCountry + "-" + rule.ToCountry, FXDate: fx.Date, RulesetVersion: rule.RulesetVersion}

	if rule.FromCountry != in.FromCountry || rule.ToCountry != in.ToCountry {
		return res, fmt.Errorf("rule corridor %s-%s does not match input %s-%s",
			rule.FromCountry, rule.ToCountry, in.FromCountry, in.ToCountry)
	}
	destCcy, err := CurrencyFor(rule.ToCountry)
	if err != nil {
		return res, err
	}
	srcCcy, err := CurrencyFor(rule.FromCountry)
	if err != nil {
		return res, err
	}
	_ = srcCcy
	res.DestCurrency = destCcy

	rateSrc, err := fx.Rate(in.Currency)
	if err != nil {
		return res, err
	}
	rateDst, err := fx.Rate(destCcy)
	if err != nil {
		return res, err
	}

	stale := time.Since(rule.VerifiedAt).Hours()/24.0 >= MaxRuleAgeDays
	if stale {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("tax rule last verified %d days ago (>180) — re-verification pending", int(time.Since(rule.VerifiedAt).Hours()/24)))
	}

	// 1. item price → destination currency, via EUR base
	if !in.ItemPrice.IsPositive() {
		return res, fmt.Errorf("item price must be positive")
	}
	itemDst := in.ItemPrice.Div(rateSrc).Mul(rateDst)
	itemDst = twoDP(itemDst)
	res.Lines = append(res.Lines, Line{
		Label:  fmt.Sprintf("Item price (%s %s → %s)", in.ItemPrice.StringFixed(2), in.Currency, destCcy),
		Amount: itemDst,
		Basis:  fmt.Sprintf("ECB reference FX %s", fx.Date),
	})

	// 2. FX card spread — stated, its own line
	spreadPct := money.MustDecimal(CardSpreadPct)
	spread := twoDP(itemDst.Mul(spreadPct).Div(money.MustDecimal("100")))
	res.Lines = append(res.Lines, Line{
		Label:  fmt.Sprintf("FX card spread (%s%%)", CardSpreadPct),
		Amount: spread,
		Basis:  "typical card FX spread, stated separately — edit assumptions if your card differs",
	})

	// 3. shipping (already in destination currency)
	shipping := twoDP(in.Shipping)
	basis := "user-provided assumption"
	if shipping.IsZero() {
		shipping = money.MustDecimal("180") // default assumption — surfaced, editable
		basis = "default assumption — EDIT ME"
		res.Warnings = append(res.Warnings, "shipping defaulted to "+shipping.StringFixed(2)+" "+destCcy+" — replace with your quote")
	}
	res.Lines = append(res.Lines, Line{Label: "Shipping + handling", Amount: shipping, Basis: basis})

	// 4. insurance
	insPct := rule.InsurancePct
	if in.InsurancePct.IsPositive() {
		insPct = in.InsurancePct
	}
	insurance := twoDP(itemDst.Mul(insPct).Div(money.MustDecimal("100")))
	res.Lines = append(res.Lines, Line{
		Label:  fmt.Sprintf("Shipping insurance (%s%%)", insPct.StringFixed(2)),
		Amount: insurance,
		Basis:  "declared-value cover, percentage of item — user-editable",
	})

	// customs value = CIF
	cif := itemDst.Add(spread).Add(shipping).Add(insurance)
	cif = twoDP(cif)
	res.Lines = append(res.Lines, Line{
		Label:  fmt.Sprintf("Customs value (CIF) — HS %s", rule.HsCode),
		Amount: cif,
		Basis:  "item + FX spread + shipping + insurance",
	})

	// 5. duty
	duty := twoDP(cif.Mul(rule.DutyRate).Div(money.MustDecimal("100")))
	res.Lines = append(res.Lines, Line{
		Label:  fmt.Sprintf("Import duty (%s%%)", rule.DutyRate.StringFixed(2)),
		Amount: duty,
		Basis:  rule.Basis + " — " + rule.SourceURL,
		Stale:  stale,
	})

	// 6. VAT / sales-tax line
	overAsk := money.Zero
	if !in.AskPrice.IsPositive() {
		in.AskPrice = in.ItemPrice
	}
	askDst := twoDP(in.AskPrice.Div(rateSrc).Mul(rateDst))
	switch {
	case rule.VatBasis == "none" || rule.VatRate.IsZero():
		res.Lines = append(res.Lines, Line{
			Label:  "Import VAT",
			Amount: money.Zero,
			Basis:  "no import VAT for this corridor — state/local taxes are out of scope (see warning)",
		})
		if rule.ToCountry == "US" {
			res.Warnings = append(res.Warnings, "US state sales tax applies at delivery and is not included — it varies by state")
		}
		res.Total = twoDP(cif.Add(duty))
	default:
		vat := twoDP(cif.Add(duty).Mul(rule.VatRate).Div(money.MustDecimal("100")))
		res.Lines = append(res.Lines, Line{
			Label:  fmt.Sprintf("Import VAT (%s%% on CIF+duty)", rule.VatRate.StringFixed(2)),
			Amount: vat,
			Basis:  rule.Basis + " — " + rule.SourceURL,
			Stale:  stale,
		})
		res.Total = twoDP(cif.Add(duty).Add(vat))
	}

	// margin scheme assumption — always surfaced (PLAN §8 rule 3)
	if in.MarginScheme {
		res.Warnings = append(res.Warnings,
			"margin scheme: VAT may already have been settled at the watch's original EU import — private sales often work this way; verify with the seller. This estimate assumes commercial import at full VAT.")
	} else {
		res.Warnings = append(res.Warnings,
			"assumes commercial import at full VAT. Private-sale margin schemes may not apply. Estimate only — not customs advice.")
	}

	// over-ask: total vs the original ask
	if askDst.IsPositive() {
		overAsk = res.Total.Sub(askDst).Div(askDst).Mul(money.MustDecimal("100")).Round(1)
		res.OverAskPct = overAsk
	}
	return res, nil
}
