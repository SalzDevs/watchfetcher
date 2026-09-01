// Command ingest: NDJSON → SQLite observations (append-only time series).
// Usage: go run ./cmd/ingest --in nightly.ndjson --db data/pricing.sqlite
// Safe to re-run: dedup on (source_type, native_id, observed_at) via INSERT OR IGNORE.
// FX via env: GBPUSD, EURUSD, CHFUSD (defaults 1.27/1.08/1.12).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"watchfetcher/internal/model"
	"watchfetcher/internal/store"
)

func main() {
	inPath := flag.String("in", "", "NDJSON input path (default: stdin)")
	dbPath := flag.String("db", "data/pricing.sqlite", "path to pricing sqlite")
	batchTimeStr := flag.String("batch-time", "", "observed_at RFC3339 (default: now UTC, truncated to second)")
	flag.Parse()

	batchTime := time.Now().UTC()
	if *batchTimeStr != "" {
		t, err := time.Parse(time.RFC3339, *batchTimeStr)
		if err != nil {
			fatal("parse --batch-time:", err)
		}
		batchTime = t.UTC()
	}
	// Truncate to second so re-ingesting same batch with same second is idempotent.
	batchTime = batchTime.Truncate(time.Second)

	rates := store.DefaultRates
	if v := os.Getenv("GBPUSD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rates.GBP = f
		}
	}
	if v := os.Getenv("EURUSD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rates.EUR = f
		}
	}
	if v := os.Getenv("CHFUSD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			rates.CHF = f
		}
	}

	var f *os.File
	var err error
	if *inPath == "" || *inPath == "-" {
		f = os.Stdin
	} else {
		f, err = os.Open(*inPath)
		if err != nil {
			fatal("open in:", err)
		}
		defer f.Close()
	}

	var listings []model.Listing
	scanner := bufio.NewScanner(f)
	// NDJSON lines can be large (image URLs); 2MB buf.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	lines, bad := 0, 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lines++
		var l model.Listing
		if err := json.Unmarshal(line, &l); err != nil {
			bad++
			continue
		}
		// Skip listings without stable identity or price — not attributable.
		if l.Source == "" || l.NativeID == "" || l.Price <= 0 {
			bad++
			continue
		}
		listings = append(listings, l)
	}
	if err := scanner.Err(); err != nil {
		fatal("scan:", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open db:", err)
	}
	defer db.Close()

	inserted, err := store.InsertObservations(db, listings, batchTime, rates)
	if err != nil {
		fatal("insert:", err)
	}
	total, _ := store.CountObservations(db)
	fmt.Printf("ingest: read %d lines (%d bad/skipped), inserted %d new observations at %s (GBP %.3f EUR %.3f CHF %.3f), total %d\n",
		lines, bad, inserted, batchTime.Format(time.RFC3339), rates.GBP, rates.EUR, rates.CHF, total)
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
