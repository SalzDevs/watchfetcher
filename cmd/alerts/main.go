// Command alerts: the outbound retention loop (PLAN.md §5.3).
// Run nightly after the engine: every watched reference with genuinely new
// ledger activity gets one honest email. Nothing new = nothing sent.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"watchledger/internal/alerts"
	"watchledger/internal/auth"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	baseURL := flag.String("base-url", envOr("BASE_URL", "http://localhost:8080"), "public base URL")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		fatal("open:", err)
	}
	defer db.Close()

	mailer := pickMailer()
	res, err := alerts.Run(db, mailer, *baseURL, timeNow())
	if err != nil {
		fatal("alerts:", err)
	}
	fmt.Printf("alerts: %d watchers, %d sent, %d skipped (nothing new)\n", res.Watchers, res.Sent, res.Skipped)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func pickMailer() auth.Mailer {
	return &auth.LogMailer{Prefix: "[alerts]"}
}

func timeNow() time.Time { return time.Now().UTC() }

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}
