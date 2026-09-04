// Command web: the WatchLedger HTTP surface (read models only).
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"watchledger/internal/auth"
	"watchledger/internal/httpx"
	"watchledger/internal/store"
)

func main() {
	dbPath := flag.String("db", "data/watchledger.sqlite", "ledger database path")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	if env := os.Getenv("PORT"); env != "" && *addr == ":8080" {
		*addr = ":" + strings.TrimPrefix(env, ":")
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open %s: %v", *dbPath, err)
	}
	defer db.Close()
	if err := store.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	srv := httpx.New(db)
	// production mail: set SMTP_* + BASE_URL. Dev default: links print to
	// stdout ([dev-mail]). A managed provider plugs in behind auth.Mailer.
	if base := os.Getenv("BASE_URL"); base != "" {
		srv.SetMail(mailerFromEnv(), base)
	}
	log.Printf("watchledger web listening on %s db=%s", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

// mailerFromEnv — SMTP when configured; stdout otherwise (never blocks dev).
// A managed provider (Postmark/SES/Loops) plugs in behind auth.Mailer unchanged.
func mailerFromEnv() auth.Mailer {
	if os.Getenv("SMTP_HOST") != "" {
		log.Println("mail: SMTP delivery not implemented yet — falling back to stdout mailer (dev)")
	}
	return auth.NewLogMailer()
}
