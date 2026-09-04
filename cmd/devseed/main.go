package main

import (
	"flag"
	"fmt"
	"time"

	"watchledger/internal/ledger"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	flag.Parse()
	db, err := store.Open(*dbPath)
	if err != nil {
		panic(err)
	}
	if err := store.Migrate(db); err != nil {
		panic(err)
	}
	db.Exec(`DELETE FROM observations`)
	db.Exec(`DELETE FROM verdict_content`)
	for _, src := range []string{"auction_a", "auction_b", "chrono24"} {
		db.Exec(`INSERT OR IGNORE INTO sources (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
			VALUES (?, ?, 'approved', 'smoke', strftime('%s','now'), 'smoke', 1)`, src, src)
	}
	now := time.Now().UTC()
	for i := 0; i < 12; i++ {
		src := []string{"auction_a", "auction_b", "chrono24"}[i%3]
		_, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID: src, Kind: "auction_realised",
			Brand: "Rolex", Model: "Submariner Date", Ref: "126610LN", Dial: "black", Material: "steel",
			Title: fmt.Sprintf("Submariner lot %d", i), URL: fmt.Sprintf("https://example.com/%d", i),
			Price: fmt.Sprintf("%d", 13800+i*100), Currency: "USD", PriceUSD: fmt.Sprintf("%d", 13800+i*100),
			ObservedAt: now.Add(-time.Duration(5+i*7) * 24 * time.Hour),
		})
		if err != nil {
			panic(err)
		}
	}
	// asks for the spread block (context data — never a verdict input, Phase 0 relabel)
	askPrices := []string{"15200", "15500", "15800", "16200", "17100", "14900"}
	for i, p := range askPrices {
		_, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID: "ebay", Kind: "ask",
			Brand: "Rolex", Model: "Submariner Date", Dial: "black", Material: "steel", Ref: "126610LN",
			Title: "Rolex Submariner Date 126610LN", URL: fmt.Sprintf("https://www.ebay.com/itm/%d", 700+i),
			Price: p, Currency: "USD", PriceUSD: p,
			ObservedAt: now.Add(-time.Duration(i*3) * 24 * time.Hour),
		})
		if err != nil {
			panic(err)
		}
	}

	fmt.Println("seeded", *dbPath)
}
