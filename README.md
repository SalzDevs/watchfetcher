# WatchLedger

> **Paste the link. We'll tell you if the price is fair — and show you why.**

The reproducible verdict engine for pre-owned watches. Realised prices from
rights-approved sources, a landed-cost engine, and verdicts that re-derive
byte-for-byte from stored evidence plus a versioned ruleset — forever.

- Blueprint: [PLAN.md](PLAN.md) — the single source of truth.
- Execution: [docs/PHASES.md](docs/PHASES.md) + [docs/phases/](docs/phases/).
- Decisions: [docs/decisions.md](docs/decisions.md).

## Stack (deliberately boring — see decisions)

Go 1.25 · SQLite (WAL, Litestream-backed) · server-rendered html/template + htmx ·
shopspring/decimal for money · zero ORM (sqlc-style typed queries) · no JS framework.

## Layout

```
cmd/web         HTTP surface (read models only — compute never here)
cmd/engine      nightly compute: ledger → content-addressed verdicts
cmd/ingest      rights-gated source ingestion (sources.enabled is a DB fact)
cmd/reproduce   the harness: any verdict re-derives or CI fails
cmd/gengolden   golden suite generator (ruleset changes only)
cmd/devseed     dev-only smoke data
internal/engine pure compute — no I/O imports (G1)
internal/ledger append-only write paths (raw docs, observations, verdicts)
internal/catalogue reference resolution cascade
internal/store  SQLite + embedded migrations; rights constraint in schema
```

## Invariants (violating any of these is a bug)

1. Compute never touches the network; ingest never produces a verdict.
2. Observations and raw documents are append-only. No UPDATE paths exist.
3. A source cannot be enabled without rights evidence in the DB — enforced by CHECK constraint.
4. Every verdict stores `inputs_hash` + `ruleset_hash`. Same inputs + same rules = same verdict.
5. Money is decimal, always. Money is never a JSON number.
6. No range without passing all four gates. "We don't know yet" ships.
7. No LLM in the verdict path. Determinism is the product.

## Commands

```
make test        # all tests incl. golden suite
make engine      # recompute verdicts from the ledger
make reproduce   # verify every verdict re-derives — trust incident if not
make run         # serve on :8080
go run ./cmd/devseed && make engine && make reproduce   # smoke
```

## Golden suite

`internal/engine/golden.json` holds frozen observation sets and their frozen
verdicts. Regenerate **only** with a deliberate ruleset version bump:

```
go run ./cmd/gengolden && git diff internal/engine/golden.json
```

An unexplained diff is a silent pricing-logic change — the one failure mode
that ends this company's credibility. It is made structurally loud.

---

Legacy `watchfetcher` (marketplace-scraping prototype) lives on branch
`archive/legacy-v0` (tag `archive-v0`). Retired per PLAN §4/§13.
