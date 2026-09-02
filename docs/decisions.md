# Decision log — engineering ADRs

Companion to PLAN.md §19 (strategy decisions). These are stack/architecture
decisions; changes require a new entry, never an edit.

---

## D1 — Single language: Go. Single binary. No web framework.

**2026-09-01.** Replaces the legacy watchfetcher dual-stack (Go API + Next.js web).

- Context: solo founder, audit-first product, content-heavy UI, near-zero infra budget.
- Decision: everything in Go. Server-rendered `html/template` + htmx for the UI.
- Why not Next/React: duplicates domain types across two runtimes (the v0 mistake),
  SPA rot risk, JS-required pages on a trust/SEO product. htmx + templates cover
  every interaction the plan needs (vault picker, calculator, briefs via print CSS).
- Why not Python/Django: weaker single-binary story on Fly; admin panel appeal
  not worth a second language for the engine's determinism/testability needs.
- Why not Rust: iteration speed for a solo founder; Go's stdlib covers sha256/
  sql/json/HTTP with zero deps of consequence.

## D2 — SQLite on a Fly volume, Litestream to object storage.

**2026-09-01.**

- Context: "the ledger is the company" — durability beats scale. 10⁴–10⁶ rows/year.
- Decision: SQLite WAL on the Fly volume; Litestream ships WAL to S3-compatible
  storage continuously. Point-in-time restore from day 1.
- Why not Postgres: managed DB = recurring cost + a vendor in every rights
  conversation. SQLite with Litestream is boring, free, and fast enough for years.
- Consequence: single-writer. Fine — one ingest path, nightly compute.

## D3 — sqlc-style typed queries, no ORM.

**2026-09-01.** SQL is the source of truth (matches G6: policy in schema).
Query helpers are thin and hand-typed until the schema stabilises; sqlc adoption
planned at Phase 2 when query count grows.

## D4 — Money is `shopspring/decimal`; JSON money is a string.

**2026-09-01.** Float never crosses a money field (G7). Ruleset multipliers are
configuration, not money — float64 allowed there, hashed into the ruleset.

## D5 — Scraper retirement.

**2026-09-01.** PLAN.md §4/§13 are absolute. v0 marketplace scrapers archived
(`archive/legacy-v0`), nightly cron killed on prod (2026-08-31), prod asks snapshot
preserved (`data/archive/pricing-asks-snapshot-2026-08-31.sqlite`, 10,440 observations).
Ask observations may re-enter the new ledger later ONLY with a rights basis, labelled
`kind='ask'`, never an auction-realised verdict input.

## D6 — Content-addressed verdicts from day 0.

**2026-09-01.** `verdict_content(inputs_hash, ruleset_hash) PK`. The legacy
mutable `verdicts` table is not recreated. History comes free from hashing, not
from a history table.

## D7 — Gates ship as reported status, strict flip is dated (F1).

**2026-09-01.** Phase 0 reports `gates_status` but the public strict mode (404 on
`limited`) waits for realised data (Phase 2). Shipping an empty product mid-phase
helps nobody; mislabelling asks as evidence helps less. F1 is per-family.

## D8 — htmx is self-hosted, vendored into the binary.

**2026-09-01.** One 14KB file, no CDN dependency, works offline, forever.
