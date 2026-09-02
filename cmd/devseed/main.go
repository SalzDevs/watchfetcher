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
	fmt.Println("seeded /tmp/watchledger-smoke.sqlite")
}
