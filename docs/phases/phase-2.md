# Phase 2 — Auction Ingestion + Reference Pages

> Status: **PLANNED.** Source: PLAN.md §4 (Tier 1), §6, §7.1–7.3, §9, §10, §11.
> Depends on: Phase 0 (ledger, resolver, gates, rulesets). Benefits from Phase 1 traffic.
> Duration: 3–5 weeks. This is the credibility layer — realised prices, the plan's Tier 1.

---

## 2.1 Source adapters — public auction records

**Objective.** Rights-clean realised prices. Phillips, Christie's, Sotheby's, Bonhams publish results — public record, permanent, citable.

**Design.**

```go
// internal/sourcesv2 — v1 scrapers stay frozen; this is the compliant line.
type Source interface {
  ID() string
  Cadence() time.Duration
  Fetch(ctx context.Context, since time.Time) ([]RawDoc, error) // bytes + url, nothing parsed
}

type Extractor interface {
  Extract(raw []byte) ([]CandidateLot, error) // pure; fixture-testable (G1)
}

type CandidateLot struct {
  RefGuess    string   // raw string as printed ('126610LN', 'Ref. 5711/1A')
  BrandGuess  string
  ModelText   string
  HammerPrice decimal.Decimal
  Currency    string
  Premium     decimal.Decimal // buyer's premium, separated per PLAN §6 normalise rule
  SaleDate    time.Time
  LotURL      string
  RawDocHash  string
}
```

**Tasks.**

1. Register houses in `sources` with `access_status='approved', rights_basis='public_record'`, reviewer = founder, links to each house's published-results page in `rights_basis` text.
2. Adapters: one per house. Start with **one** (whichever results archive is plainest HTML — decide at build time by inspection, not guesswork), prove the pipeline, then replicate.
3. Extraction fixtures: 20 stored lots per house committed under `internal/sourcesv2/testdata/`. Extractor changes must re-extract fixtures identically or consciously update fixtures.
4. Dedup: `(house, sale_date, lot_number)` natural key → content hash → observation append (G2).
5. Premium handling: store hammer + premium separately; ledger observation records `price_realised` = hammer + premium (total paid) AND components. Statistics run on total paid — that is what a buyer pays.

**Acceptance.**

- 200 lots sampled: raw→facts re-extraction is byte-identical.
- Every observation row joins to a `raw_documents.id` (provenance chain, G3).

---

## 2.2 Reference resolver in production

**Objective.** Candidate lot text → canonical reference, via cascade, never guesswork (PLAN §7.1).

**Cascade implementation.**

| Rung | Method | Confidence | Action |
|---|---|---|---|
| 1 | Structured field from house data (some houses expose ref) | 1.00 | accept |
| 2 | Exact normalised ref match in catalogue | 0.98 | accept |
| 3 | Alias hit (catalogue_aliases) | 0.90 | accept |
| 4 | Fuzzy similarity ≥0.70 AND brand match | 0.70 | **review queue, never auto-accept** |
| 5 | nothing | — | unresolved → review queue |

**Tasks.**

1. `internal/resolve` package: `Resolve(text, brand) Resolution{Ref, Confidence, Rung}`. Fuzzy = Levenshtein/Jaro on normalised token sets, brand-gated.
2. Table `review_queue(id, candidate_lot_id, resolver_output jsonb, status open|resolved|rejected, resolved_ref, resolved_by, resolved_at)`.
3. Minimal admin UI (`/admin/review`, single-user, basic auth): show lot text + top-3 fuzzy candidates → pick/refuse. Every resolution writes `catalogue_aliases` (kind='market_name') — precision compounds (PLAN §7.1).
4. Metric: auto-accept precision sampled 200/2,000 lots; target ≥0.85 on rungs 1–3.

**Acceptance.** No observation enters the ledger at confidence <0.85 without a human row. Sampled precision target met.

---

## 2.3 Reference pages (the SEO body)

**Objective.** Permanent, factual, indexable pages per reference — the existing vault UX repositioned (PLAN §9).

**Scope discipline (PLAN §10):** **five families only.** Rolex Submariner, Rolex Datejust, Omega Speedmaster Professional, Tudor Black Bay, Seiko SKX/Alpinist. All other families render "not yet covered" — thin coverage destroys the trust thesis.

**Page anatomy (`/references/[ref]`).**

1. **Realised block** — exact-tier auction range: p10–p90 + median (PLAN §7.5: never mean, never single value), counts, house diversity, gate status. Reuses Caliper.
2. **Evidence table** — every comparable: house, sale date, lot link, hammer, premium, total, tier badge.
3. **Exclusions block** — VARIANT/RELATED/EXCLUDED lots **shown with reasons** ("aftermarket diamond bezel — excluded", PLAN §7.2: visible exclusions are a trust signal).
4. **Vault** — sibling references within the family (existing picker, re-sourced from realised ledger).
5. **Spread block** — rendered only after Phase 3 (before that, completely absent — not a greyed placeholder).
6. **Methodology footer** — link to methodology page + ruleset hash of the numbers shown.
7. Landed-cost teaser → corridor tool (Phase 1 cross-link).

**Data pipeline.**

- Nightly: recompute reference pages' read-model from `verdict_content` + ledger. Precompute; page render is a lookup, not a computation.

**Acceptance.**

- Every number on every reference page re-derives via `cmd/reproduce` (Phase 0 harness).
- Gate-failing references show the counts + gate names, never a range.
- Lighthouse ≥95; JSON-LD `Product`+`Offer`AggregateRating avoided (no rating claims — editorial rule).

---

## 2.4 Methodology page

**Objective.** Public rules, always current, auditable (PLAN §13 editorial rules).

**Tasks.**

1. `/methodology`: rendered from `rulesets.definition` + gate thresholds + adjustment catalogues (when they exist, Phase 4). Generated at build from the same rows the engine reads — **text and behaviour cannot diverge**.
2. Dated changelog section from `rulesets.created_at` history.
3. Statement block: "Dealers can never pay to improve a public verdict" — permanent (PLAN §13).

**Acceptance.** A non-engineer can read the page and restate why a given reference shows no number.

---

## Flip point F1 (executes here, family by family)

When a family's realised ledger passes gates, ask-derived ranges **leave the public surface** for that family. Ask observations stay in the ledger (they fuel Phase 3 spread). This is the plan-purist completion of the Phase 0 relabel.

## Non-goals

- No user-facing filtering/sorting beyond family navigation.
- No pre-2020 deep backfill beyond what a house's public archive gives for free.
- No Seiko adapter if SKX/Alpinist results prove too sparse after first crawl — substitute per PLAN §10 spirit (a fifth family with real depth), documented in decision log.

## Risks

| Risk | Mitigation |
|---|---|
| House sites block fetching | These are public records; robots-respecting, low-cadence (daily), identified UA with contact. If one blocks: drop it, three houses ≥ gate diversity |
| Sparse per-ref results → most pages gate-fail | That IS the product (honesty promise). Ship counts. Density grows with each sale season |
| Ref extraction quality too low for vintage lots | Review queue is the designed answer; budget curator time (PLAN §17: first hire is a curator) |

## Exit criteria

- 5 families live: ≥20 references each with a page, backed by ≥100 stored realised-price observations across ≥2 houses.
- `cmd/reproduce` green across all published ranges.
- First organic inbound (forum quote, newsletter mention) attributable to a reference page or corridor page.
