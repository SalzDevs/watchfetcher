package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"watchledger/internal/store"
)

// Full pipeline over real fixture bytes: fetch-sim (bytes given) → raw store →
// extract → resolve → ledger. Bonhams 31330 is an Old-Paintings sale — nothing
// resolves into the Phase 2 families; the run must be honest about it (0
// appended, catalogue gaps queued, nothing fabricated).
func TestIngestAuctionOutOfFamily(t *testing.T) {
	db := setup(t)

	raw, err := os.ReadFile("../sourcesv2/testdata/bonhams-auction-31330.html")
	if err != nil {
		t.Fatal(err)
	}
	report, err := IngestAuction(db, "bonhams", "31330", raw, time.Now().UTC())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if report.Lots < 20 {
		t.Fatalf("expected ≥20 lots, got %d", report.Lots)
	}
	if report.Appended != 0 {
		t.Fatalf("paintings sale must not append watch observations — got %d", report.Appended)
	}
	if report.Unsold == 0 {
		t.Fatal("fixture has unsold lots — expected honest counting")
	}

	// raw document stored exactly once (provenance, G3)
	var docs int
	db.QueryRow(`SELECT COUNT(*) FROM raw_documents WHERE source_id='bonhams'`).Scan(&docs)
	if docs != 1 {
		t.Fatalf("want 1 raw doc, got %d", docs)
	}

	// re-ingest: idempotent ledger (append-only, dedup by content hash)
	report2, err := IngestAuction(db, "bonhams", "31330", raw, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if report2.Appended != 0 {
		t.Fatalf("re-ingest must not append (no duplicates possible when nothing appended)")
	}
}

// G6: disabled source refuses to run, even with bytes in hand.
func TestIngestRefusesDisabledSource(t *testing.T) {
	db := setup(t)
	raw, _ := os.ReadFile("../sourcesv2/testdata/bonhams-auction-31330.html")
	db.Exec(`UPDATE sources SET enabled = 0 WHERE id = 'bonhams'`)
	if _, err := IngestAuction(db, "bonhams", "31330", raw, time.Now()); err == nil {
		t.Fatal("disabled source must fail the run")
	}
}

func setup(t *testing.T) *sql.DB {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	// Bonhams registered: public record, approved, enabled (as migration 0006 does)
	if _, err := db.Exec(`UPDATE sources SET enabled = 1 WHERE id = 'bonhams'`); err != nil {
		t.Fatal(err)
	}
	return db
}

// in-family path: synthetic sale where lots resolve into scope
func TestIngestAuctionInFamily(t *testing.T) {
	db := setup(t)

	html := pageNames([]string{
		"Rolex Submariner Date Ref 126610LN, boxed",
		"Rolex Submariner Date 126610LN, unworn",
		"Rolex Submariner Date ref. 126610LN full set",
		"Rolex Submariner Date 126610LN",
		"Rolex Submariner Date 126610LN",
		"Rolex Submariner Date 126610LN",
		"Rolex Submariner Date 126610LN",
		"Rolex Submariner Date 126610LN",
		"Tudor Black Bay 58 79030N",
	})
	fixture := buildAuctionPage(html, "39999", "2026-08-15T11:00:00+00:00")

	report, err := IngestAuction(db, "bonhams", "39999", []byte(fixture), time.Now().UTC())
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if report.Appended < 8 {
		t.Fatalf("expected ≥8 in-family observations, got %+v", report)
	}

	// ledger rows carry provenance + resolution
	var conf float64
	var kind string
	if err := db.QueryRow(`SELECT resolution_confidence, kind FROM observations LIMIT 1`).Scan(&conf, &kind); err != nil {
		t.Fatal(err)
	}
	if kind != "auction_realised" || conf < 0.85 {
		t.Fatalf("resolution quality wrong: kind=%s conf=%f", kind, conf)
	}

	// reference page read model has data
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM observations WHERE UPPER(ref)='126610LN'`).Scan(&n)
	if n < 8 {
		t.Fatalf("ref observations missing: %d", n)
	}
}

// ---- helpers: build a synthetic __NEXT_DATA__ page in the real shape ----

func pageNames(names []string) []string { return names }

func buildAuctionPage(names []string, auctionID, saleDate string) string {
	var lots string
	for i, name := range names {
		lot := map[string]any{
			"lotId": fmt.Sprint(100 + i), "lotItemId": fmt.Sprint(9000 + i),
			"lotNo": map[string]any{"full": fmt.Sprint(i + 1)},
			"name":  name, "title": name, "slug": fmt.Sprintf("lot-%d", i),
			"brand":     "test",
			"auctionId": auctionID, "status": "SOLD",
			"currency":          map[string]any{"iso_code": "EUR", "bonhams_code": "€"},
			"hammerTime":        map[string]any{"datetime": saleDate},
			"price":             map[string]any{"hammerPrice": 5000 + i*100, "hammerPremium": 1250},
			"styledDescription": name,
		}
		b, _ := json.Marshal(lot)
		lots += string(b) + ","
	}
	page := fmt.Sprintf(`<!doctype html><html><head></head><body>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"feed":[%s]}}}</script>
</body></html>`, strings.TrimRight(lots, ","))
	return page
}
