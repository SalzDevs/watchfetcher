// Command seedcatalogue: bulk-seed the catalogue from a JSON export.
// Shape: {"refs":[[ref,brand,family,dial,material,version,updated_at],...],
//         "aliases":[[alias,ref,kind,added_by,created_at],...],
//         "queue":[[source_id,raw_doc_id,candidate,resolver_output],...]}
// Idempotent (INSERT OR IGNORE except queue rows, which keep their own identity).
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	path := flag.String("in", "", "JSON export path")
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	flag.Parse()
	if *path == "" {
		fmt.Println("--in required")
		os.Exit(2)
	}
	f, err := os.ReadFile(*path)
	if err != nil {
		fatal(err)
	}
	var in struct {
		Refs    [][]any             `json:"refs"`
		Aliases [][]any             `json:"aliases"`
		Queue   [][]json.RawMessage `json:"queue"`
	}
	if err := json.Unmarshal(f, &in); err != nil {
		fatal(err)
	}
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	for _, r := range in.Refs {
		if _, err := db.Exec(`INSERT OR IGNORE INTO catalogue_references
			(ref, brand, family, dial, material, version, updated_at) VALUES (?,?,?,?,?,?,?)`,
			r[0], r[1], r[2], r[3], r[4], r[5], r[6]); err != nil {
			fatal(err)
		}
	}
	for _, a := range in.Aliases {
		if _, err := db.Exec(`INSERT OR IGNORE INTO catalogue_aliases
			(alias, ref, kind, added_by, created_at) VALUES (?,?,?,?,?)`,
			a[0], a[1], a[2], a[3], a[4]); err != nil {
			fatal(err)
		}
	}
	queued := 0
	for _, q := range in.Queue {
		if _, err := db.Exec(`INSERT INTO review_queue (source_id, raw_doc_id, candidate, resolver_output)
			VALUES (?, NULL, ?, ?)`, q[0], q[2], q[3]); err != nil {
			fmt.Println("queue skip:", err) // may already exist — fine
			continue
		}
		queued++
	}
	fmt.Printf("seeded: %d refs, %d aliases, %d queued (of %d)\n", len(in.Refs), len(in.Aliases), queued, len(in.Queue))
}

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
