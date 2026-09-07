package curator

import (
	"path/filepath"
	"testing"

	"watchledger/internal/landedcost"
	"watchledger/internal/money"
	"watchledger/internal/store"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		in       string
		house    string
		date     string
		price    string
		currency string
		refs     int
	}{
		{"Phillips 2026-03-12 Rolex Submariner 126610LN CHF 14,000", "phillips", "2026-03-12", "14000", "CHF", 1},
		{"Christies 12 Mar 2026 Nautilus 5711/1A-010 sold for £12,500", "christies", "2026-03-12", "12500", "GBP", 1},
		{"Sothebys Mar 12, 2026 Speedmaster 310.30.42.50.01.001 USD 5,800", "sothebys", "2026-03-12", "5800", "USD", 1},
		{"Antiquorum 05/04/2026 Omega 2254.50.00 EUR 2.400", "antiquorum", "2026-04-05", "2400", "EUR", 1},
	}
	for _, c := range cases {
		p := ParseLine(c.in)
		if p.House != c.house || p.SaleDate != c.date || p.Price != c.price || p.Currency != c.currency {
			t.Errorf("%q → house=%q date=%q price=%q ccy=%q", c.in, p.House, p.SaleDate, p.Price, p.Currency)
		}
		if len(p.RefCands) != c.refs {
			t.Errorf("%q → refs=%v", c.in, p.RefCands)
		}
	}
}

func TestParseLineMissingFields(t *testing.T) {
	p := ParseLine("some vague text about a watch")
	if p.House != "" || p.SaleDate != "" || p.Price != "" {
		t.Fatalf("vague text must parse empty: %+v", p)
	}
}

// full flow against a real DB: parse → preview → ledger → observation exists
func TestLedgerFlow(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	fx := landedcost.FX{Date: "2026-09-01", Rates: map[string]money.Decimal{
		"CHF": money.MustDecimal("0.94"), "USD": money.MustDecimal("1.09"),
	}}

	p := ParseLine("Phillips 2026-03-12 Rolex Submariner Date 126610LN CHF 14,000")
	pv, err := PreviewRow(db, fx, p, map[string]bool{"Submariner Date": true})
	if err != nil {
		t.Fatal(err)
	}
	if !pv.Resolved || pv.Ref != "126610LN" || pv.Brand != "Rolex" {
		t.Fatalf("preview wrong: %+v", pv)
	}
	if !pv.InScope {
		t.Fatal("Submariner Date must be in scope")
	}
	if pv.PriceUSD == "" {
		t.Fatal("no USD preview")
	}

	obsID, err := Ledger(db, "phillips", p, pv.Ref, fx)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if obsID == 0 {
		t.Fatal("no observation id")
	}
	var kind, ref, priceUSD string
	if err := db.QueryRow(`SELECT kind, ref, price_usd FROM observations WHERE id = ?`, obsID).
		Scan(&kind, &ref, &priceUSD); err != nil {
		t.Fatal(err)
	}
	if kind != "auction_realised" || ref != "126610LN" || priceUSD == "" {
		t.Fatalf("observation wrong: kind=%s ref=%s usd=%s", kind, ref, priceUSD)
	}

	// ledgering the same line twice → dedup (append-only, content hash)
	if _, err := Ledger(db, "phillips", p, pv.Ref, fx); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM observations WHERE ref = '126610LN'`).Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate ledger must dedup, got %d rows", n)
	}
}
