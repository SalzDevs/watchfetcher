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
	// realised tier only: asks/delists are context (PLAN §7.2), never verdict inputs
	written, limited, count, err := ledger.RunCompute(db, time.Now().UTC())
	if err != nil {
		fatal("compute:", err)
	}
	fmt.Printf("engine: %d realised observations in ledger, %d verdicts written (%d limited by gates) under ruleset %s\n",
		count, written, limited, engine.Current.Version)
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
