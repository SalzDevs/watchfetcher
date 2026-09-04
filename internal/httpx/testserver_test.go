package httpx

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"watchledger/internal/ledger"
	"watchledger/internal/store"
)

// testServer — migrated temp DB with a gate-clean realised set + asks for
// ref 126610LN (the fixture watch), wrapped in the real router.
func testServer(t *testing.T) *Server {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	seedEvidence(t, db)
	return New(db)
}

func seedEvidence(t *testing.T, db *sql.DB) {
	t.Helper()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	for _, src := range []string{"auction_a", "auction_b", "chrono24"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO sources
			(id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
			VALUES (?, ?, 'approved', 'test', strftime('%s','now'), 'test', 1)`, src, src); err != nil {
			t.Fatal(err)
		}
	}

	// 12 realised comps — gate-clean (3 sources, fresh, tight band)
	for i := 0; i < 12; i++ {
		src := []string{"auction_a", "auction_b", "chrono24"}[i%3]
		price := fmt.Sprintf("%d", 13800+i*100)
		if _, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID: src, Kind: "auction_realised",
			Brand: "Rolex", Model: "Submariner Date", Dial: "black", Material: "steel", Ref: "126610LN",
			Title: fmt.Sprintf("Submariner comp %d", i), URL: fmt.Sprintf("https://example.com/%d", i),
			Price: price, Currency: "USD", PriceUSD: price,
			ObservedAt: now.Add(-time.Duration(5+i*7) * 24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// 6 asks — spread context
	asks := []string{"15200", "15500", "15800", "16200", "17100", "14900"}
	for i, p := range asks {
		if _, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID: "ebay", Kind: "ask",
			Brand: "Rolex", Model: "Submariner Date", Dial: "black", Material: "steel", Ref: "126610LN",
			Title: "Submariner ask", URL: fmt.Sprintf("https://ebay.com/%d", i),
			Price: p, Currency: "USD", PriceUSD: p,
			ObservedAt: now.Add(-time.Duration(i*3) * 24 * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
}
