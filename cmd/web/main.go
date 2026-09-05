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

// mailerFromEnv — real SMTP when configured (SMTP_HOST/PORT/USER/PASS/MAIL_FROM);
// stdout otherwise (dev). Set BASE_URL too: the magic link must point at a
// reachable host or the email is useless.
func mailerFromEnv() auth.Mailer {
	if os.Getenv("SMTP_HOST") != "" {
		port := envOr("SMTP_PORT", "587")
		log.Printf("mail: SMTP via %s:%s", os.Getenv("SMTP_HOST"), port)
		return &auth.SMTPMailer{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     port,
			User:     os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			From:     envOr("MAIL_FROM", "WatchLedger <hello@watchfairvalue.com>"),
		}
	}
	log.Println("mail: no SMTP_HOST — login links print to stdout (dev mode)")
	return auth.NewLogMailer()
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
