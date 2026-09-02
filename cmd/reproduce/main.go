// Command reproduce: the reproducibility harness (PLAN.md §13, G8).
// Recomputes a verdict from stored evidence + the stored ruleset anchor time
// and compares the published subset byte-for-byte against verdict_content.
//
// Usage:
//
//	go run ./cmd/reproduce --db data/watchledger.sqlite --cell 'rolex|submariner date|black|steel|'
//	go run ./cmd/reproduce --db ... --all
//
// Exit 0 = reproducible. Exit 1 = trust incident; stop and investigate.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"watchledger/internal/engine"
	"watchledger/internal/ledger"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	cellKey := flag.String("cell", "", "cell key to verify")
	all := flag.Bool("all", false, "verify every stored verdict")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open:", err)
	}
	defer db.Close()

	if err := ledger.SaveRuleset(db, engine.Current); err != nil {
		fatal("save ruleset:", err)
	}

	cells, err := targetCells(db, *cellKey, *all)
	if err != nil {
		fatal("list cells:", err)
	}

	bad, checked := 0, 0
	for _, cell := range cells {
		result, err := ledger.LoadLatestVerdictContent(db, cell, engine.Current.Hash())
		if err != nil {
			fatal("load verdict:", err)
		}
		if result == "" {
			fmt.Printf("SKIP %s: no stored verdict under current ruleset\n", cell)
			continue
		}
		checked++

		var stored engine.Verdict
		if err := json.Unmarshal([]byte(result), &stored); err != nil {
			fatal("parse stored verdict:", err)
		}

		obs, err := ledger.LoadCellObservations(db, cell)

		if err != nil {
			fatal("load observations:", err)
		}

		p := splitCell(cell)
		get := func(i int) string {
			if i < len(p) {
				return p[i]
			}
			return ""
		}
		recomputed := engine.ComputeVerdict(get(0), get(1), get(2), get(3), get(4), obs, stored.ComputedAt)

		if recomputed == nil {
			fmt.Printf("FAIL %s: stored verdict exists but evidence no longer computes\n", cell)
			bad++
			continue
		}
		if mismatch(stored, *recomputed) {
			fmt.Printf("FAIL %s:\n  stored:    p10=%s median=%s p90=%s n=%d gates=%s %v\n",
				cell, stored.P10, stored.Median, stored.P90, stored.Count, stored.GatesStatus, stored.FailingGates)
			fmt.Printf("  recomputed: p10=%s median=%s p90=%s n=%d gates=%s %v\n",
				recomputed.P10, recomputed.Median, recomputed.P90, recomputed.Count, recomputed.GatesStatus, recomputed.FailingGates)
			bad++
			continue
		}
		fmt.Printf("ok   %s (%d comps, median %s)\n", cell, recomputed.Count, recomputed.Median)
	}

	if checked == 0 {
		fmt.Println("no stored verdicts to verify (ledger empty — run the engine first)")
		return
	}
	if bad > 0 {
		fmt.Printf("\n%d/%d verdict(s) NOT reproducible — trust incident (PLAN.md §14)\n", bad, checked)
		os.Exit(1)
	}
	fmt.Printf("\nall %d checked verdicts reproduce exactly\n", checked)
}

func targetCells(db *sql.DB, cell string, all bool) ([]string, error) {
	if !all && cell != "" {
		return []string{cell}, nil
	}
	rows, err := db.Query(`SELECT DISTINCT cell_key FROM verdict_content ORDER BY cell_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// mismatch compares the published subset. Receipts are excluded: they are
// evidence presentation, not the claim. The claim = range + counts + gates.
func mismatch(a, b engine.Verdict) bool {
	return a.Count != b.Count ||
		!a.Median.Equal(b.Median) || !a.P10.Equal(b.P10) || !a.P90.Equal(b.P90) ||
		a.GatesStatus != b.GatesStatus || len(a.FailingGates) != len(b.FailingGates)
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

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
