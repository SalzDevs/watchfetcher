package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"watchledger/internal/landedcost"
	"watchledger/internal/money"
	"watchledger/internal/sourcesv2"
)

type ebaySearchItem = sourcesv2.EBayItem

func stubSearch(rawResp string) Searcher {
	return func(ctx context.Context, q string) ([]sourcesv2.EBayItem, json.RawMessage, error) {
		return searchItems(rawResp)
	}
}

func searchItems(rawResp string) ([]sourcesv2.EBayItem, json.RawMessage, error) {
	var parsed struct {
		ItemSummaries []sourcesv2.EBayItem `json:"itemSummaries"`
	}
	if err := json.Unmarshal([]byte(rawResp), &parsed); err != nil {
		return nil, nil, err
	}
	return parsed.ItemSummaries, json.RawMessage(rawResp), nil
}

func landedcostFX() landedcost.FX {
	return landedcost.FX{
		Date:  "2026-09-01",
		Rates: map[string]money.Decimal{"USD": money.MustDecimal("1.09"), "EUR": money.MustDecimal("1.0")},
	}
}

func IngestEBayAsksWithSearcher(db *sql.DB, s Searcher, fx landedcost.FX, now time.Time) (IngestReport, error) {
	return IngestEBayAsks(db, s, fx, now)
}

// fixture in the real eBay item_summary response shape
func ebayResponse(items []map[string]any) string {
	b, _ := json.Marshal(map[string]any{"itemSummaries": items})
	return string(b)
}

func ebayItem(id, title, price string) map[string]any {
	return map[string]any{
		"itemId":     "v1|" + id + "|0",
		"title":      title,
		"price":      map[string]any{"value": price, "currency": "USD"},
		"itemWebURL": "https://www.ebay.com/itm/" + id,
		"condition":  "Pre-Owned",
	}
}

// asks land in the ledger resolved by title refs, scoped to Phase 2 families.
func TestEBayAsksIngest(t *testing.T) {
	db := setup(t)
	fx := landedcostFX()

	// the real eBay filters by q — the stub must too (per-family queries)
	resp := ebayResponse([]map[string]any{
		ebayItem("111", "Rolex Submariner Date 126610LN Black Dial", "15000"),
		ebayItem("112", "Rolex Submariner Date 126610LN box papers", "15500"),
		ebayItem("113", "Patek Philippe Nautilus 5711/1A-010", "90000"),
		ebayItem("114", "A lovely vintage watch, no reference given", "500"),
	})
	searcher := func(ctx context.Context, q string) ([]ebaySearchItem, json.RawMessage, error) {
		items, raw, err := searchItems(resp)
		if err != nil {
			return nil, nil, err
		}
		var filtered []sourcesv2.EBayItem
		for _, it := range items {
			// naive q-match: query words must appear in the title
			match := true
			for _, w := range strings.Fields(q) {
				if !strings.Contains(strings.ToLower(it.Title), strings.ToLower(w)) {
					match = false
					break
				}
			}
			if match {
				filtered = append(filtered, it)
			}
		}
		return filtered, raw, nil
	}

	report, err := IngestEBayAsksWithSearcher(db, searcher, fx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if report.Appended != 2 {
		t.Fatalf("want 2 asks appended (in-scope only), got %+v", report)
	}
	// 5711 (Nautilus) is never even searched: the scope query only asks the
	// five in-scope families. OutOfScope stays 0 by construction (PLAN §10).
	if report.OutOfScope != 0 || report.NoRef != 0 {
		t.Fatalf("out-of-scope families must never be queried: %+v", report)
	}

	// ledger rows are asks
	var kind string
	var price string
	if err := db.QueryRow(`SELECT kind, price_usd FROM observations WHERE ref='126610LN' LIMIT 1`).Scan(&kind, &price); err != nil {
		t.Fatal(err)
	}
	if kind != "ask" || price != "15000.00" {
		t.Fatalf("ask row wrong: kind=%s price=%s", kind, price)
	}
}

// delist-diff: item seen once, absent on a later run (past the 48h grace) →
// delist observation appended, ebay_items marked delisted. Re-appearing items
// never fabricate delists.
func TestEBayDelistDiff(t *testing.T) {
	db := setup(t)
	fx := landedcostFX()
	now := time.Now().UTC()

	run1 := ebayResponse([]map[string]any{
		ebayItem("211", "Rolex Submariner Date 126610LN", "15000"),
		ebayItem("212", "Rolex Submariner Date 126610LN", "15200"),
	})
	if _, err := IngestEBayAsksWithSearcher(db, stubSearch(run1), fx, now); err != nil {
		t.Fatal(err)
	}

	// run 2 (30h later — within grace): item 211 vanished → NO delist yet
	run2 := ebayResponse([]map[string]any{ebayItem("212", "Rolex Submariner Date 126610LN", "15200")})
	if _, err := IngestEBayAsksWithSearcher(db, stubSearch(run2), fx, now.Add(30*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var delists int
	db.QueryRow(`SELECT COUNT(*) FROM observations WHERE kind='delist'`).Scan(&delists)
	if delists != 0 {
		t.Fatalf("grace window must suppress delists, got %d", delists)
	}

	// run 3 (72h after run1): 211 still gone → delist event
	if _, err := IngestEBayAsksWithSearcher(db, stubSearch(run2), fx, now.Add(72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM observations WHERE kind='delist'`).Scan(&delists)
	if delists != 1 {
		t.Fatalf("want exactly 1 delist, got %d", delists)
	}
	var ref string
	db.QueryRow(`SELECT ref FROM observations WHERE kind='delist'`).Scan(&ref)
	if ref != "126610LN" {
		t.Fatalf("delist ref wrong: %q", ref)
	}
	var marked int
	db.QueryRow(`SELECT COUNT(*) FROM ebay_items WHERE delisted_at IS NOT NULL`).Scan(&marked)
	if marked != 1 {
		t.Fatalf("ebay_items must mark delisted: %d", marked)
	}

	// idempotence: running again must not double-delist
	if _, err := IngestEBayAsksWithSearcher(db, stubSearch(run2), fx, now.Add(96*time.Hour)); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM observations WHERE kind='delist'`).Scan(&delists)
	if delists != 1 {
		t.Fatalf("delist must be append-once, got %d", delists)
	}
}

// no credentials → honest failure, never a silent empty run
func TestEBayNoCredentialsFails(t *testing.T) {
	c := sourcesv2.NewEBayClientFromEnv("", "")
	if _, _, err := c.Search(context.Background(), "rolex", 10); err == nil {
		t.Fatal("missing credentials must error")
	} else if !strings.Contains(err.Error(), "EBAY_CLIENT_ID") {
		t.Fatalf("wrong error: %v", err)
	}
}
