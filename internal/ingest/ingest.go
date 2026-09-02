// Package ingest: the rights-approved ingestion pipeline (PLAN.md §6).
// fetch → store raw → extract → resolve → append observation.
// Resolved lots outside the Phase-2 family scope are skipped, not guessed.
package ingest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"watchledger/internal/catalogue"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/sourcesv2"
	"watchledger/internal/store"
)

// Phase 2 scope discipline (PLAN.md §10): five families only.
var PhaseScope = map[string]bool{
	"Submariner Date":          true,
	"Submariner No-Date":       true,
	"Datejust":                 true,
	"Speedmaster Professional": true,
	"Black Bay":                true,
	"Black Bay 58":             true,
}

// IngestAuction runs the full pipeline over raw auction-page bytes that were
// fetched from a rights-approved source. Testable offline: bytes in, ledger rows out.
func IngestAuction(db *sql.DB, sourceID, auctionID string, raw []byte, fetchedAt time.Time) (IngestReport, error) {
	var report IngestReport

	enabled, err := store.SourceEnabled(db, sourceID)
	if err != nil {
		return report, err
	}
	if !enabled {
		return report, fmt.Errorf("source %q is not enabled — rights evidence required (G6)", sourceID)
	}

	rawHash, _, err := ledger.SaveRawDocument(db, sourceID, "https://www.bonhams.com/auction/"+auctionID+"/", "text/html", raw, fetchedAt)
	if err != nil {
		return report, err
	}
	_ = rawHash

	auction, err := sourcesv2.ExtractAuction(raw, auctionID)
	if err != nil {
		return report, err
	}
	report.Lots = len(auction.Lots)

	fx, err := landedcost.LoadFX(db, "latest")
	if err != nil {
		return report, err
	}

	rawDocID := rawDocID(db, sourceID, rawHash)
	for _, lot := range auction.Lots {
		if !lot.Sold {
			report.Unsold++
			continue
		}
		if lot.Currency == "" {
			report.SkippedNoCurrency++
			continue
		}
		candidates := sourcesv2.ExtractRefCandidates(lot.Name + " " + lot.Description)
		if len(candidates) == 0 {
			report.NoRef++
			continue
		}

		var resolved catalogue.Resolution
		resolvedAny := false
		for _, cand := range candidates {
			r, err := catalogue.Lookup(db, cand)
			if err != nil {
				return report, err
			}
			if r.NeedsReview || r.Ref == "" {
				continue
			}
			resolved = r
			resolvedAny = true
			break
		}

		if !resolvedAny {
			// refs printed but no catalogue hit → catalogue gap → review queue
			if err := queueForReview(db, sourceID, rawDocID, lot, candidates); err != nil {
				return report, err
			}
			report.Queued++
			continue
		}

		if !PhaseScope[resolved.Family] {
			report.OutOfScope++
			continue
		}

		total := lot.HammerPrice.Add(lot.HammerPremium)
		usd, err := landedcost.ConvertToUSD(fx, lot.Currency, total)
		if err != nil {
			report.SkippedNoCurrency++
			continue
		}

		isNew, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID:             sourceID,
			Kind:                 "auction_realised",
			Brand:                resolved.Brand,
			Model:                resolved.Family,
			Dial:                 resolved.Dial,
			Material:             resolved.Material,
			Ref:                  resolved.Ref,
			ResolutionConfidence: resolved.Confidence,
			ResolutionRung:       resolved.Rung,
			Title:                lot.Name,
			URL:                  lot.URL,
			RawDocID:             rawDocID,
			Price:                total.StringFixed(2),
			Currency:             lot.Currency,
			PriceUSD:             usd.StringFixed(2),
			ObservedAt:           lot.SaleDate,
		})
		if err != nil {
			return report, err
		}
		if isNew {
			report.Appended++
		} else {
			report.Duplicates++
		}
	}
	log.Printf("ingest %s/%s: %+v", sourceID, auctionID, report)
	return report, nil
}

type IngestReport struct {
	Lots              int `json:"lots"`
	Unsold            int `json:"unsold"`
	NoRef             int `json:"no_ref"`
	Queued            int `json:"queued"`
	OutOfScope        int `json:"out_of_scope"`
	SkippedNoCurrency int `json:"skipped_no_currency"`
	Appended          int `json:"appended"`
	Duplicates        int `json:"duplicates"`
}

func rawDocID(db *sql.DB, sourceID, hash string) sql.NullInt64 {
	var id int64
	err := db.QueryRow(`SELECT id FROM raw_documents WHERE content_hash = ?`, hash).Scan(&id)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: id, Valid: true}
}

func queueForReview(db *sql.DB, sourceID string, rawDocID sql.NullInt64, lot sourcesv2.Lot, candidates []string) error {
	cand, _ := json.Marshal(map[string]any{
		"title": lot.Name, "url": lot.URL, "ref_candidates": candidates,
		"price": lot.HammerPrice.String(), "currency": lot.Currency,
	})
	_, err := db.Exec(`
		INSERT INTO review_queue (source_id, raw_doc_id, candidate)
		VALUES (?, ?, ?)`, sourceID, rawDocID, string(cand))
	return err
}

var _ = money.Zero
