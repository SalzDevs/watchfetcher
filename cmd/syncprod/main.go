// Command syncprod (ops): bulk-sync catalogue + observations from a JSON
// export into the prod ledger. Append-only on observations (content hash
// dedup). Deletes verdict_content so the next engine run recomputes clean.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	inPath := flag.String("in", "", "JSON export path")
	dbPath := flag.String("db", "/data/watchledger.sqlite", "ledger database path")
	purgeSources := flag.String("purge-sources", "", "comma-separated source_ids whose observations + verdicts must be deleted (data hygiene)")
	flag.Parse()

	if *purgeSources != "" {
		db, err := sql.Open("sqlite", *dbPath)
		if err != nil {
			panic(err)
		}
		defer db.Close()
		for _, src := range strings.Split(*purgeSources, ",") {
			res, err := db.Exec(`DELETE FROM observations WHERE source_id = ?`, strings.TrimSpace(src))
			if err != nil {
				panic(err)
			}
			n, _ := res.RowsAffected()
			fmt.Printf("purged %d observations from %q\n", n, src)
		}
		db.Exec(`DELETE FROM verdict_content`)
		fmt.Println("verdict_content cleared — run engine to recompute clean")
		return
	}
	if *inPath == "" {
		fmt.Println("--in required")
		os.Exit(2)
	}
	f, err := os.ReadFile(*inPath)
	if err != nil {
		panic(err)
	}
	var in struct {
		Refs    [][]any `json:"refs"`
		Aliases [][]any `json:"aliases"`
		Obs     [][]any `json:"obs"`
	}
	if err := json.Unmarshal(f, &in); err != nil {
		panic(err)
	}
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	for _, r := range in.Refs {
		db.Exec(`INSERT OR IGNORE INTO catalogue_references (ref, brand, family, dial, material, version, updated_at) VALUES (?,?,?,?,?,?,?)`, r...)
	}
	for _, a := range in.Aliases {
		db.Exec(`INSERT OR IGNORE INTO catalogue_aliases (alias, ref, kind, added_by, created_at) VALUES (?,?,?,?,?)`, a...)
	}
	appended := 0
	for _, o := range in.Obs {
		observedAt := int64(0)
		switch v := o[15].(type) {
		case float64:
			observedAt = int64(v)
		}
		res, err := db.Exec(`INSERT OR IGNORE INTO observations
			(source_id, kind, brand, model, dial, material, scope, ref, resolution_confidence, resolution_rung, title, url, price, currency, price_usd, observed_at, content_hash)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			o[0], o[1], o[2], o[3], o[4], o[5], o[6], o[7], o[8], o[9], o[10], o[11], o[12], o[13], o[14], observedAt, o[16])
		if err != nil {
			fmt.Println("obs skip:", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			appended++
		}
	}
	db.Exec(`DELETE FROM verdict_content`)
	_ = strconv.Itoa(0)
	fmt.Printf("syncprod: %d refs, %d aliases, %d observations appended; verdict_content cleared\n", len(in.Refs), len(in.Aliases), appended)
}
