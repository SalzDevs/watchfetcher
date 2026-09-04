// Command ingest: rights-approved source ingestion (PLAN.md §6).
// fetch → store raw → extract → resolve → ledger. Never produces a verdict (G1).
//
// Usage:
//
//	go run ./cmd/ingest --source bonhams \
//	  --auction https://www.bonhams.com/auction/31330/weekly-watches/ \
//	  --auction https://www.bonhams.com/auction/30600/weekly-watches/
//
// The source must be rights-approved + enabled in the sources table (G6):
//
//	UPDATE sources SET enabled=1, access_status='approved',
//	  rights_basis='public_record: published auction results pages',
//	  rights_reviewed_at=strftime('%s','now'), reviewer='<name>' WHERE id='bonhams';
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"watchledger/internal/ingest"
	"watchledger/internal/sourcesv2"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	source := flag.String("source", "bonhams", "source id (must be approved + enabled in sources)")
	var auctions auctionURLs
	fetchLots := flag.Bool("fetch-lots", false, "enrich no-ref lots by fetching their lot pages (rate-limited)")
	lotDelay := flag.Int("lot-delay-ms", 700, "delay between lot-page fetches")
	flag.Var(&auctions, "auction", "auction results page URL (repeatable)")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open db:", err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		fatal("migrate:", err)
	}

	if len(auctions) == 0 {
		fatal("--auction required (repeatable), e.g. https://www.bonhams.com/auction/31330/weekly-watches/")
	}

	total := ingest.IngestReport{}
	for _, url := range auctions {
		auctionID := extractAuctionID(url)
		if auctionID == "" {
			fatal(fmt.Sprintf("cannot parse auction id from %q", url))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		raw, _, err := sourcesv2.FetchRaw(ctx, url)
		cancel()
		if err != nil {
			fatal("fetch:", err)
		}
		var report ingest.IngestReport
		if *fetchLots {
			fetcher := func(ctx context.Context, lotURL string) ([]byte, error) {
				ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
				defer cancel()
				body, _, err := sourcesv2.FetchRaw(ctx, lotURL)
				return body, err
			}
			var err error
			report, err = ingest.IngestAuctionWithEnrichment(db, *source, auctionID, raw, time.Now().UTC(), fetcher, time.Duration(*lotDelay)*time.Millisecond)
			if err != nil {
				fatal(fmt.Sprintf("ingest %s:", auctionID), err)
			}
		} else {
			var err error
			report, err = ingest.IngestAuction(db, *source, auctionID, raw, time.Now().UTC())
			if err != nil {
				fatal(fmt.Sprintf("ingest %s:", auctionID), err)
			}
		}
		if err != nil {
			fatal(fmt.Sprintf("ingest %s:", auctionID), err)
		}
		total.Appended += report.Appended
		total.Duplicates += report.Duplicates
		total.Queued += report.Queued
		total.OutOfScope += report.OutOfScope
		total.Unsold += report.Unsold
		total.NoRef += report.NoRef
		total.Lots += report.Lots
	}
	fmt.Printf("\ningest totals: %d lots, %d appended, %d duplicates, %d queued (catalogue gaps), %d out of scope, %d unsold, %d no-ref\n",
		total.Lots, total.Appended, total.Duplicates, total.Queued, total.OutOfScope, total.Unsold, total.NoRef)
}

func extractAuctionID(url string) string {
	// https://www.bonhams.com/auction/31330/weekly-watches/ → 31330
	parts := strings.Split(strings.TrimRight(url, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if isDigits(parts[i]) {
			return parts[i]
		}
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type auctionURLs []string

func (a *auctionURLs) String() string { return strings.Join(*a, ", ") }
func (a *auctionURLs) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
