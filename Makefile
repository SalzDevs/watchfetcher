.PHONY: build test migrate run engine reproduce seed tidy

build:
	go build -o bin/web ./cmd/web
	go build -o bin/engine ./cmd/engine
	go build -o bin/ingest ./cmd/ingest
	go build -o bin/reproduce ./cmd/reproduce

test:
	go test ./... -count=1

DB ?= data/watchledger.sqlite

migrate:
	go run ./cmd/web --db $(DB) --addr :18080 & sleep 1; kill %1

run:
	go run ./cmd/web --db $(DB)

engine:
	go run ./cmd/engine --db $(DB)

reproduce:
	go run ./cmd/reproduce --db $(DB) --all

alerts:
	go run ./cmd/alerts --db $(DB) --base-url ${BASE_URL:-http://localhost:8080}

seed: ## dev-only: seed a smoke ledger into $(DB)
	go run ./cmd/devseed --db $(DB)

tidy:
	go mod tidy
