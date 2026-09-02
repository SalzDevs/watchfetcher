// Command web: the WatchLedger HTTP surface (read models only).
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

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
	log.Printf("watchledger web listening on %s db=%s", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
