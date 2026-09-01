# WatchFetcher

Go crawler for WatchFairValue: fetches pre-owned watch listings from four
marketplaces nightly and writes canonical NDJSON → SQLite → verdicts.

## Sources
- Chrono24 (JSON-LD offers, Chrome TLS impersonation) — most WAF-sensitive
- Bob's Watches (JSON-LD Product blocks, brand-pathed catalog)
- The 1916 Company (Salesforce SCAPI via guest SLAS PKCE)
- Watchfinder (server-rendered Magento catalogsearch, GBP)

## Pipeline (cumulative, idempotent)

```
fetcher (80/model, polite) → nightly.ndjson → ingest (FX → price_usd, append-only) → pricing.sqlite:observations → engine → verdicts
```

* **Polite:** per-source rate limits (Chrono24 3.2s, Watchfinder 2.5s, Bob's 1.8s, 1916 0.9s) + ±30% jitter, shuffle jobs, 400ms stagger, 3 concurrent, retry 3× on 429/503 with backoff. Chrono24 bans don't hammer — circuit breaker skips remaining jobs.
* **Idempotent:** `ingest` dedups on `(source_type, native_id, observed_at)` — re-running same batch inserts 0. `engine` preserves previous verdicts if new batch has no comparable data (e.g. Chrono24 banned overnight).
* **FX:** GBP→USD (default 1.27), EUR 1.08, CHF 1.12, overridable via `GBPUSD/EURUSD/CHFUSD` env.

## Usage

```bash
# Nightly (Fly.io): single command, handles everything, never IP-bans
./scripts/run-nightly.sh
# Or step-by-step:
go run ./cmd/fetcher --config config/models.json --max-per-model 80 --out data/nightly.ndjson --metrics data/crawl_metrics.json
go run ./cmd/ingest --in data/nightly.ndjson --db data/pricing.sqlite
go run ./cmd/engine --db data/pricing.sqlite
```

Output: one JSON object per listing — source, native_id, url, title,
brand/model/ref, dial/material/scope (when stated), price + currency.
Unknown attributes are omitted, never guessed.

## Monitoring (Fly.io, zero-cost)

No proxy needed. Ban detection is file+log based:

```bash
# Check if Chrono24 is currently flagging us
fly logs --machine nightly -n 100 | grep ALERT
# Or inspect metrics JSON on volume
fly ssh console -C "cat /data/crawl_metrics.json | jq"
cat data/crawl_metrics.json | jq '.sources.chrono24'
```

Metrics at `data/crawl_metrics.json` (`--metrics` flag):
`jobs_total/ok/err`, `http_200/403/429/503`, `banned` (circuit open), `last_error`, `duration_s`.
`ALERT` lines appear in stderr when `banned:true` or `403>=3` or `chrono24` 0-ok.

If Chrono24 is banned one night, `engine` keeps previous verdicts — no data loss, next night auto-retries.

## Fly.io Deploy

```bash
fly volumes create data --region mad --size 1
fly deploy  # builds Dockerfile (Go 1.25, compiled binaries)
fly machine run --schedule daily --volume data:/data /app/scripts/run-nightly.sh
# Test locally before deploy:
./scripts/run-nightly.sh
cat data/crawl_metrics.json
```

See `fly.toml.example` and `Dockerfile`. Env `DB_PATH`, `NDJSON_PATH`, `METRICS_PATH` override paths.

## Cron

`scripts/run-nightly.sh` does: `flock` lock → 0-30 min jitter → fetcher (80/model) → ingest → engine → log metrics + ALERT. Keeps last 7 NDJSON files. Exits 0 even on partial fetch (verdicts preserved).
