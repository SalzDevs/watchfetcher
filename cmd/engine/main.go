// Command engine: the compute layer. Reads the observation ledger, groups by
// exact cell, computes content-addressed verdicts, stores them for the web
// read models. Pure → storage, never the network (G1). Never publishes a
// verdict that fails the evidence gates — those store as "limited" and the
// UI refuses to render a range (G5, PLAN.md §7.4).
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open:", err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		fatal("migrate:", err)
	}
	if err := ledger.SaveRuleset(db, engine.Current); err != nil {
		fatal("save ruleset:", err)
	}

	// all observations, grouped by cell in memory (SQLite local — fine at ledger scale)
	rows, err := db.Query(`
		SELECT brand, model, dial, material, scope, ref, kind, source_id,
		       title, url, price_usd, observed_at
		FROM observations WHERE price_usd IS NOT NULL`)
	if err != nil {
		fatal("read observations:", err)
	}
	defer rows.Close()

	cells := map[string][]engine.Observation{}
	count := 0
	for rows.Next() {
		var o engine.Observation
		var priceUSD string
		var observedAt int64
		if err := rows.Scan(&o.Brand, &o.Model, &o.Dial, &o.Material, &o.Scope, &o.Ref,
			&o.Kind, &o.Source, &o.Title, &o.URL, &priceUSD, &observedAt); err != nil {
			fatal("scan:", err)
		}
		d, err := moneyFromString(priceUSD)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: corrupt price %q skipped: %v\n", priceUSD, err)
			continue
		}
		o.PriceUSD = d
		o.ObservedAt = time.Unix(observedAt, 0)
		key := engine.CellKey(o.Brand, o.Model, o.Dial, o.Material, o.Scope)
		cells[key] = append(cells[key], o)
		count++
	}
	rows.Close()
	fmt.Printf("engine: %d observations in %d cells\n", count, len(cells))

	now := time.Now().UTC()
	written, limited := 0, 0
	for key, obs := range cells {
		p := splitCell(key)
		v := engine.ComputeVerdict(p[0], p[1], p[2], p[3], p[4], obs, now)
		if v == nil {
			continue
		}
		if err := ledger.SaveVerdictContent(db, v); err != nil {
			fatal("save verdict:", err)
		}
		written++
		if v.GatesStatus == "limited" {
			limited++
		}
	}
	fmt.Printf("engine: %d verdicts written (%d limited by gates) under ruleset %s\n", written, limited, engine.Current.Version)
}

func splitCell(cell string) []string {
	var out []string
	cur := ""
	for _, r := range cell {
		if r == '|' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func moneyFromString(s string) (money.Decimal, error) {
	return money.FromString(s)
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

var _ = sql.ErrNoRows
