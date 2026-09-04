// Package ingest: the rights-approved ingestion pipeline (PLAN.md §6).
// fetch → store raw → extract → resolve → ledger. Never produces a verdict (G1).
//
// Two-stage reference discovery:
//  1. title + styledDescription patterns (cheap, on the auction page),
//  2. lot-page enrichment — lots without candidates get their lot page fetched
//     (rate-limited) and the meta description re-scanned ("Reference: 3131").
//
// The lot-page fetcher is injectable: tests stub it, cmd/ingest uses the network.
package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"watchledger/internal/catalogue"
	"watchledger/internal/landedcost"
	"watchledger/internal/ledger"
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

// LotPageFetcher fetches one lot page's bytes. cmd/ingest supplies the network;
// tests supply fixtures.
type LotPageFetcher func(ctx context.Context, url string) ([]byte, error)

// Pipeline carries the dependencies. Now anchors engine time; FX overrides tests.
type Pipeline struct {
	DB           *sql.DB
	SourceID     string
	FX           landedcost.FX
	Now          time.Time
	FetchLotPage LotPageFetcher // nil = no enrichment stage
	LotPageDelay time.Duration  // politeness between enrichment fetches

	rawDocID sql.NullInt64 // auction-page provenance
}

// IngestReport is the honest run summary.
type IngestReport struct {
	Lots              int `json:"lots"`
	Unsold            int `json:"unsold"`
	NoRef             int `json:"no_ref"`
	Enriched          int `json:"enriched"` // refs found via lot-page stage
	Queued            int `json:"queued"`   // catalogue gaps → review queue
	OutOfScope        int `json:"out_of_scope"`
	SkippedNoCurrency int `json:"skipped_no_currency"`
	Appended          int `json:"appended"`
	Duplicates        int `json:"duplicates"`
}

// IngestAuction — pipeline without enrichment (offline tests, cheap runs).
func IngestAuction(db *sql.DB, sourceID, auctionID string, raw []byte, fetchedAt time.Time) (IngestReport, error) {
	p := &Pipeline{DB: db, SourceID: sourceID, Now: fetchedAt}
	return p.run(auctionID, raw)
}

// IngestAuctionWithEnrichment — pipeline + lot-page enrichment stage.
func IngestAuctionWithEnrichment(db *sql.DB, sourceID, auctionID string, raw []byte, fetchedAt time.Time, fetch LotPageFetcher, delay time.Duration) (IngestReport, error) {
	p := &Pipeline{DB: db, SourceID: sourceID, Now: fetchedAt, FetchLotPage: fetch, LotPageDelay: delay}
	return p.run(auctionID, raw)
}

func (p *Pipeline) run(auctionID string, raw []byte) (IngestReport, error) {
	var report IngestReport

	enabled, err := store.SourceEnabled(p.DB, p.SourceID)
	if err != nil {
		return report, err
	}
	if !enabled {
		return report, fmt.Errorf("source %q is not enabled — rights evidence required (G6)", p.SourceID)
	}

	rawHash, _, err := ledger.SaveRawDocument(p.DB, p.SourceID,
		"https://www.bonhams.com/auction/"+auctionID+"/", "text/html", raw, p.Now)
	if err != nil {
		return report, err
	}
	p.DB.QueryRow(`SELECT id FROM raw_documents WHERE content_hash = ?`, rawHash).Scan(&p.rawDocID)

	auction, err := sourcesv2.ExtractAuction(raw, auctionID)
	if err != nil {
		return report, err
	}
	report.Lots = len(auction.Lots)

	fx := p.FX
	if fx.Date == "" {
		if fx, err = landedcost.LoadFX(p.DB, "latest"); err != nil {
			return report, err
		}
	}

	for _, lot := range auction.Lots {
		if !lot.Sold {
			report.Unsold++
			continue
		}
		if lot.Currency == "" {
			report.SkippedNoCurrency++
			continue
		}

		// stage 1: title + styledDescription (auction page only)
		candidates := sourcesv2.ExtractRefCandidates(lot.Name + " " + lot.Description)

		// stage 2: lot-page meta description ("Reference: 3131 …")
		if len(candidates) == 0 && p.FetchLotPage != nil && lot.URL != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			lotRaw, err := p.FetchLotPage(ctx, lot.URL)
			cancel()
			if err != nil {
				log.Printf("ingest: lot page %s failed: %v", lot.URL, err)
			} else {
				if _, _, err := ledger.SaveRawDocument(p.DB, p.SourceID, lot.URL, "text/html", lotRaw, p.Now); err != nil {
					return report, err
				}
				desc, err := sourcesv2.ExtractLotMetaDescription(lotRaw)
				if err != nil {
					return report, err
				}
				if candidates = sourcesv2.ExtractRefCandidates(lot.Name + " " + desc); len(candidates) > 0 {
					report.Enriched++
					log.Printf("ingest: enriched %s via lot page: %v", lot.LotNumber, candidates)
				}
				if p.LotPageDelay > 0 {
					time.Sleep(p.LotPageDelay)
				}
			}
		}

		if len(candidates) == 0 {
			report.NoRef++
			continue
		}

		// resolver cascade over the candidates — first catalogue hit wins
		var resolved catalogue.Resolution
		resolvedAny := false
		var considered []string
		for _, cand := range candidates {
			r, err := catalogue.Lookup(p.DB, cand)
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
			if err := p.queueForReview(lot, candidates, considered); err != nil {
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

		isNew, err := ledger.AppendObservation(p.DB, ledger.Observation{
			SourceID:             p.SourceID,
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
			RawDocID:             p.rawDocID,
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
	log.Printf("ingest %s/%s: %+v", p.SourceID, auctionID, report)
	return report, nil
}

// queueForReview — refs printed but no catalogue hit = a catalogue gap.
// The queue feeds aliases + new references (PLAN.md §7.1: precision compounds).
func (p *Pipeline) queueForReview(lot sourcesv2.Lot, candidates, considered []string) error {
	cand, _ := json.Marshal(map[string]any{
		"title": lot.Name, "url": lot.URL,
		"ref_candidates": candidates, "resolver_tried": considered,
		"price": lot.HammerPrice.String(), "currency": lot.Currency,
		"sale_date": lot.SaleDate.Format("2006-01-02"),
	})
	_, err := p.DB.Exec(`
		INSERT INTO review_queue (source_id, raw_doc_id, candidate, resolver_output)
		VALUES (?, ?, ?, ?)`, p.SourceID, p.rawDocID, string(cand), mustJSON(considered))
	return err
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
