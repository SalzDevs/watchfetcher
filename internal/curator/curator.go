// Package curator: the founder's data-entry tool (PLAN.md §17 — the founder
// IS the curator until a data hire). Paste raw auction-result lines from
// public archives; parse → resolve → preview → ledger with one click.
// Discipline: every row is human-confirmed before it touches the ledger.
package curator

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"watchledger/internal/catalogue"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/sourcesv2"
)

// Known houses (public record). Order matters for text matching.
var Houses = []string{"phillips", "christies", "sothebys", "bonhams", "antiquorum"}

var (
	reDateISO  = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	reDateEU   = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4})\b`)
	reDateText = regexp.MustCompile(`(?i)\b(\d{1,2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w*\s+(\d{4})\b`)
	reDateAlt  = regexp.MustCompile(`(?i)\b(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)\w*\s+(\d{1,2})\s*,?\s+(\d{4})\b`)
	rePrice    = regexp.MustCompile(`(?:USD|EUR|GBP|CHF|HKD|JPY|[$€£¥])\s*([0-9][0-9,\.]{2,12})|([0-9][0-9,\.\s]{2,12})\s*(?:USD|EUR|GBP|CHF|HKD|JPY)`)
)

var months = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

var currencyFor = map[string]string{
	"$": "USD", "€": "EUR", "£": "GBP", "¥": "JPY",
	"USD": "USD", "EUR": "EUR", "GBP": "GBP", "CHF": "CHF", "HKD": "HKD", "JPY": "JPY",
}

// Parsed is one candidate result extracted from raw text.
type Parsed struct {
	Raw        string   `json:"raw"`
	House      string   `json:"house"`
	RefCands   []string `json:"ref_candidates"`
	Price      string   `json:"price"`
	Currency   string   `json:"currency"`
	SaleDate   string   `json:"sale_date"` // YYYY-MM-DD
	PremiumPct string   `json:"premium_pct,omitempty"`
}

// ParseLine — extract house, date, price+currency, ref candidates from free
// text. Missing fields stay empty — the form allows correcting anything
// (human gate: nothing ledgers without confirmation).
func ParseLine(raw string) Parsed {
	p := Parsed{Raw: strings.TrimSpace(raw)}

	// refs FIRST (precise patterns), then remove them so their digits are
	// never misread as prices
	p.RefCands = sourcesv2.ExtractRefCandidates(p.Raw)
	work := p.Raw
	for _, r := range p.RefCands {
		work = strings.Replace(work, r, " ", 1)
	}
	low := strings.ToLower(work)

	for _, h := range Houses {
		if strings.Contains(low, h) {
			p.House = h
			break
		}
	}

	if m := reDateISO.FindStringSubmatch(p.Raw); m != nil {
		p.SaleDate = m[1] + "-" + m[2] + "-" + m[3]
	} else if m := reDateText.FindStringSubmatch(p.Raw); m != nil {
		p.SaleDate = fmt.Sprintf("%s-%02d-%02d", m[3], months[strings.ToLower(m[2])], atoi(m[1]))
	} else if m := reDateAlt.FindStringSubmatch(p.Raw); m != nil {
		p.SaleDate = fmt.Sprintf("%s-%02d-%02d", m[3], months[strings.ToLower(m[1])], atoi(m[2]))
	} else if m := reDateEU.FindStringSubmatch(p.Raw); m != nil {
		p.SaleDate = m[3] + "-" + pad(m[2]) + "-" + pad(m[1]) // DD/MM/YYYY, EU houses
	}

	if m := rePrice.FindStringSubmatch(work); m != nil {
		num, ccy := m[1], ""
		if num == "" {
			num = m[2]
			ccy = trailingCode(m[0])
		} else {
			ccy = leadingCode(m[0])
		}
		p.Currency = currencyFor[ccy]
		p.Price = cleanNumber(num)
	}

	return p
}

// Preview resolves + converts for the confirmation UI. Never ledgers.
type Preview struct {
	Parsed
	Resolved   bool    `json:"resolved"`
	Ref        string  `json:"ref"`
	Brand      string  `json:"brand"`
	Family     string  `json:"family"`
	Dial       string  `json:"dial"`
	Material   string  `json:"material"`
	Confidence float64 `json:"confidence"`
	InScope    bool    `json:"in_scope"`
	PriceUSD   string  `json:"price_usd"`
}

// PreviewRow — resolve + convert. Errors on corrupt prices only.
func PreviewRow(db *sql.DB, fx landedcost.FX, p Parsed, inScope map[string]bool) (Preview, error) {
	pv := Preview{Parsed: p}
	for _, cand := range p.RefCands {
		r, err := catalogue.Lookup(db, cand)
		if err != nil {
			return pv, err
		}
		if !r.NeedsReview && r.Ref != "" {
			pv.Resolved = true
			pv.Ref, pv.Brand, pv.Family = r.Ref, r.Brand, r.Family
			pv.Dial, pv.Material = r.Dial, r.Material
			pv.Confidence = r.Confidence
			pv.InScope = inScope[r.Family]
			break
		}
	}
	if p.Price != "" && p.Currency != "" {
		if amt, err := money.FromString(p.Price); err == nil {
			if usd, err := landedcost.ConvertToUSD(fx, p.Currency, amt); err == nil {
				pv.PriceUSD = usd.StringFixed(0)
			}
		}
	}
	return pv, nil
}

// Ledger — append the realised observation (kind='auction_realised',
// source = the house, rung 1 confidence 1.00: human-confirmed structured entry).
// Returns the observation row id for the staging link.
func Ledger(db *sql.DB, house string, p Parsed, ref string, fx landedcost.FX) (int64, error) {
	var brand, family, dial, material string
	if err := db.QueryRow(`SELECT brand, family, dial, material FROM catalogue_references WHERE ref = ?`,
		ref).Scan(&brand, &family, &dial, &material); err != nil {
		return 0, fmt.Errorf("ref %q not in catalogue: %w", ref, err)
	}

	total := p.Price
	if p.PremiumPct != "" {
		if pct, err := money.FromString(p.PremiumPct); err == nil && pct.IsPositive() {
			if base, err := money.FromString(p.Price); err == nil {
				total = base.Add(base.Mul(pct).Div(money.MustDecimal("100"))).StringFixed(2)
			}
		}
	}
	usd := ""
	if amt, err := money.FromString(total); err == nil {
		if u, err := landedcost.ConvertToUSD(fx, p.Currency, amt); err == nil {
			usd = u.StringFixed(2)
		}
	}
	saleDate, err := time.Parse("2006-01-02", p.SaleDate)
	if err != nil {
		return 0, fmt.Errorf("sale date %q: %w", p.SaleDate, err)
	}

	obs := ledger.Observation{
		SourceID: house, Kind: "auction_realised",
		Brand: brand, Model: family, Dial: dial, Material: material, Ref: ref,
		ResolutionConfidence: 1.00, ResolutionRung: 1, // human-confirmed
		Title: p.Raw,
		Price: total, Currency: p.Currency, PriceUSD: usd,
		ObservedAt: saleDate,
	}
	if _, err := ledger.AppendObservation(db, obs); err != nil {
		return 0, err
	}

	var obsID int64
	err = db.QueryRow(`SELECT id FROM observations WHERE content_hash = ?`,
		obs.ContentHash()).Scan(&obsID)
	return obsID, err
}

// ---- parsing helpers ----

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func pad(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func leadingCode(s string) string {
	s = strings.TrimSpace(s)
	for _, sym := range []string{"£", "€", "$", "¥"} {
		if strings.HasPrefix(s, sym) {
			return sym
		}
	}
	f := strings.Fields(s)
	if len(f) > 0 {
		return strings.ToUpper(f[0])
	}
	return ""
}

func trailingCode(s string) string {
	f := strings.Fields(s)
	for _, t := range f {
		for _, sym := range []string{"£", "€", "$", "¥"} {
			if strings.HasPrefix(t, sym) {
				return sym
			}
		}
		if c, ok := currencyFor[strings.ToUpper(t)]; ok {
			return c
		}
	}
	if len(f) > 0 {
		return strings.ToUpper(f[len(f)-1])
	}
	return ""
}

// cleanNumber — strip separators; "12.500" (3-digit groups) = thousands.
func cleanNumber(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	switch {
	case strings.Contains(s, ",") && !strings.Contains(s, "."):
		s = strings.ReplaceAll(s, ",", "")
	case strings.Contains(s, ".") && !strings.Contains(s, ","):
		parts := strings.Split(s, ".")
		if len(parts) > 1 && len(parts[len(parts)-1]) == 3 {
			s = strings.ReplaceAll(s, ".", "")
		}
	default:
		s = strings.ReplaceAll(s, ",", "")
	}
	return s
}

var _ = sha256.Size
var _ = hex.EncodeToString
