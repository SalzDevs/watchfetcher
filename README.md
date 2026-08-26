# WatchFetcher

Go crawler for WatchFairValue: fetches pre-owned watch listings from four
marketplaces nightly and writes canonical NDJSON.

## Sources
- Chrono24 (JSON-LD offers, Chrome TLS impersonation)
- Bob's Watches (JSON-LD Product blocks, brand-pathed catalog)
- The 1916 Company (Salesforce SCAPI via guest SLAS PKCE)
- Watchfinder (server-rendered Magento catalogsearch)

## Usage
```bash
go build -o watchfetcher ./cmd/fetcher
./watchfetcher --config config/models.json --out nightly.ndjson
```

Output: one JSON object per listing — source, native_id, url, title,
brand/model/ref, dial/material/scope (when stated), price + currency.
Unknown attributes are omitted, never guessed.
