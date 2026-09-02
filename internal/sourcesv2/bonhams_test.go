package sourcesv2

import (
	"os"
	"testing"
)

// Fixture: real page from bonhams.com/auction/31330/weekly-watches/ (past sale,
// EUR). Extraction must find the sale date + sold lots with real hammer prices
// — and must never fabricate prices for unsold lots.
func TestExtractAuctionBonhams(t *testing.T) {
	raw, err := os.ReadFile("testdata/bonhams-auction-31330.html")
	if err != nil {
		t.Fatal(err)
	}
	a, err := ExtractAuction(raw, "31330")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if a.SaleDate.IsZero() {
		t.Fatal("sale date not extracted")
	}
	if want := "2025-02-26"; a.SaleDate.Format("2006-01-02") != want {
		t.Fatalf("sale date: want %s got %s", want, a.SaleDate.Format("2006-01-02"))
	}
	if len(a.Lots) < 20 {
		t.Fatalf("expected ≥20 lots, got %d", len(a.Lots))
	}
	sold := 0
	for _, l := range a.Lots {
		if l.LotNumber == "" || l.LotID == "" || l.Currency == "" {
			t.Fatalf("lot missing identity: %+v", l)
		}
		if !l.Sold {
			if !l.HammerPrice.IsZero() {
				t.Fatalf("unsold lot %s carries a price — fabrication", l.LotNumber)
			}
			continue
		}
		sold++
		if !l.HammerPrice.IsPositive() {
			t.Fatalf("sold lot %s has no hammer price", l.LotNumber)
		}
		if l.SaleDate.IsZero() {
			t.Fatalf("sold lot %s missing hammer time", l.LotNumber)
		}
	}
	if sold < 30 {
		t.Fatalf("expected ≥30 sold lots in a finished sale, got %d", sold)
	}
	// determinism
	a2, err := ExtractAuction(raw, "31330")
	if err != nil {
		t.Fatal(err)
	}
	if len(a2.Lots) != len(a.Lots) {
		t.Fatal("extraction not deterministic")
	}
}
