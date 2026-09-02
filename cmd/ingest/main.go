// Command ingest: rights-approved sources → raw store → extractor → resolver →
// observation ledger. The ingest layer never produces a verdict (G1).
//
// Phase 0 status: pipeline shape only. A source runs solely if
// store.SourceEnabled says so — which requires rights evidence in the DB (G6).
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"

	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	source := flag.String("source", "", "source id to ingest")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open db:", err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		fatal("migrate:", err)
	}

	if *source == "" {
		fmt.Println("ingest: --source required; registered sources:")
		rows, err := db.Query(`SELECT id, name, access_status, enabled FROM sources ORDER BY id`)
		if err != nil {
			fatal("list sources:", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, name, status string
			var enabled bool
			rows.Scan(&id, &name, &status, &enabled)
			fmt.Printf("  %-18s %-22s %-10s enabled=%v\n", id, name, status, enabled)
		}
		os.Exit(0)
	}

	enabled, err := store.SourceEnabled(db, *source)
	if err != nil {
		fatal("source check:", err)
	}
	if !enabled {
		fmt.Printf("ingest: source %q is disabled — rights evidence required (PLAN.md §13).\n", *source)
		fmt.Println("ingest: nothing to do. This is the system working as designed.")
		os.Exit(0)
	}

	// Rights-approved adapters land in Phase 2 (auction houses) / Phase 3 (eBay).
	fmt.Printf("ingest: %q is enabled but no adapter is implemented yet (Phase 2).\n", *source)
	os.Exit(0)
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

var _ = sql.ErrNoRows
