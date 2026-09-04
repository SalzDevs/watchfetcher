// eBay ingestion (PLAN.md Phase 3): asks into the ledger + delist-diff
// derived from what vanished between runs (PLAN.md §6 derived signals).
// Ask observations are kind='ask' — context data, never an auction-realised
// verdict input (Phase 0 relabel).
package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"watchledger/internal/catalogue"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/sourcesv2"
	"watchledger/internal/store"
)

// Searcher abstracts the eBay client (tests stub it).
type Searcher func(ctx context.Context, query string) ([]sourcesv2.EBayItem, json.RawMessage, error)

// IngestEBayAsks — one run over the in-scope families.
func IngestEBayAsks(db *sql.DB, searcher Searcher, fx landedcost.FX, now time.Time) (IngestReport, error) {
	var report IngestReport

	enabled, err := store.SourceEnabled(db, "ebay")
	if err != nil {
		return report, err
	}
	if !enabled {
		return report, fmt.Errorf("source 'ebay' is not enabled (G6)")
	}

	// in-scope families → search queries (PLAN §10 scope holds)
	families, err := scopeFamilies(db)
	if err != nil {
		return report, err
	}

	seenThisRun := map[string]bool{}
	runStarted := now.Unix()

	for family, brand := range families {
		q := brand + " " + family
		items, rawBody, err := searcher(context.Background(), q)
		if err != nil {
			return report, fmt.Errorf("search %q: %w", q, err)
		}

		// raw document per response (G3)
		rawHash, _, err := ledger.SaveRawDocument(db, "ebay",
			fmt.Sprintf("https://api.ebay.com/buy/browse/v1/item_summary/search?q=%s", strings.ReplaceAll(q, " ", "+")),
			"application/json", rawBody, now)
		if err != nil {
			return report, err
		}
		var rawDocID sql.NullInt64
		db.QueryRow(`SELECT id FROM raw_documents WHERE content_hash = ?`, rawHash).Scan(&rawDocID)

		for _, item := range items {
			core := sourcesv2.CoreItemID(item.ItemID)
			seenThisRun[core] = true

			cands := sourcesv2.ExtractRefCandidates(item.Title)
			if len(cands) == 0 {
				report.NoRef++
				continue
			}
			var resolved catalogue.Resolution
			resolvedAny := false
			var considered []string
			for _, cand := range cands {
				r, err := catalogue.Lookup(db, cand)
				if err != nil {
					return report, err
				}
				considered = append(considered, cand+"→"+r.Ref)
				if r.NeedsReview || r.Ref == "" {
					continue
				}
				resolved = r
				resolvedAny = true
				break
			}
			if !resolvedAny {
				// asks outside the catalogue are noise, not gaps — skip
				// (auction lots earn the review queue; asks don't)
				report.NoRef++
				continue
			}
			if !PhaseScope[resolved.Family] {
				report.OutOfScope++
				continue
			}

			askUSD, err := landedcost.ConvertToUSD(fx, item.Price.Currency, money.MustDecimal(item.Price.Value))
			if err != nil {
				report.SkippedNoCurrency++
				continue
			}

			isNew, err := ledger.AppendObservation(db, ledger.Observation{
				SourceID:             "ebay",
				Kind:                 "ask",
				Brand:                resolved.Brand,
				Model:                resolved.Family,
				Dial:                 resolved.Dial,
				Material:             resolved.Material,
				Ref:                  resolved.Ref,
				ResolutionConfidence: resolved.Confidence,
				ResolutionRung:       resolved.Rung,
				Title:                item.Title,
				URL:                  item.ItemWebURL,
				RawDocID:             rawDocID,
				Price:                item.Price.Value,
				Currency:             item.Price.Currency,
				PriceUSD:             askUSD.StringFixed(2),
				ObservedAt:           now,
			})
			if err != nil {
				return report, err
			}
			if isNew {
				report.Appended++
			} else {
				report.Duplicates++
			}

			touchEBayItem(db, core, resolved.Ref, item.Title, askUSD, now)
		}
	}

	// delist-diff: items previously seen, absent this run, seen ≥48h ago
	delists, err := appendDelistEvents(db, seenThisRun, runStarted)
	report.Enriched = delists // reuse field: derived signals count
	return report, nil
}

func scopeFamilies(db *sql.DB) (map[string]string, error) {
	out := map[string]string{}
	rows, err := db.Query(`
		SELECT DISTINCT family, brand FROM catalogue_references
		WHERE family IN ('Submariner Date','Submariner No-Date','Datejust','Speedmaster Professional','Black Bay','Black Bay 58')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var family, brand string
		if rows.Scan(&family, &brand) == nil {
			out[family] = brand
		}
	}
	return out, rows.Err()
}

func touchEBayItem(db *sql.DB, itemID, ref, title string, askUSD money.Decimal, now time.Time) {
	_, _ = db.Exec(`
		INSERT INTO ebay_items (item_id, ref, title, first_seen, last_seen, last_price)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
		  last_seen = excluded.last_seen,
		  last_price = excluded.last_price,
		  title = excluded.title,
		  ref = excluded.ref`,
		itemID, ref, title, now.Unix(), now.Unix(), askUSD.StringFixed(2))
}

// appendDelistEvents — vanished asks become delist observations (append-only).
// 48h grace so a single failed fetch never fabricates a delist (G9).
func appendDelistEvents(db *sql.DB, seenThisRun map[string]bool, runStarted int64) (int, error) {
	rows, err := db.Query(`
		SELECT item_id, ref, title, last_price, last_seen FROM ebay_items
		WHERE delisted_at IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type gone struct {
		itemID, ref, title, price string
		lastSeen                  int64
	}
	var vanished []gone
	for rows.Next() {
		var g gone
		if err := rows.Scan(&g.itemID, &g.ref, &g.title, &g.price, &g.lastSeen); err != nil {
			return 0, err
		}
		if seenThisRun[g.itemID] {
			continue
		}
		if runStarted-g.lastSeen < 48*3600 {
			continue // grace window
		}
		vanished = append(vanished, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	for _, g := range vanished {
		_, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID:   "ebay",
			Kind:       "delist",
			Ref:        g.ref,
			Title:      g.title,
			URL:        "https://www.ebay.com/itm/" + g.itemID,
			PriceUSD:   g.price,
			ObservedAt: time.Unix(runStarted, 0),
		})
		if err != nil {
			return n, err
		}
		db.Exec(`UPDATE ebay_items SET delisted_at = ? WHERE item_id = ?`, runStarted, g.itemID)
		n++
	}
	return n, nil
}
