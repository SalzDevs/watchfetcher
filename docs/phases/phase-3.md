# Phase 3 — eBay API: Asks vs Realised

> Status: **PLANNED.** Source: PLAN.md §3 (the line that is the whole company), §4 (Tier 2), §6 (derived signals), §7.2.
> Depends on: Phase 2 (realised side of the comparison). Duration: 2–3 weeks.
> This is the phase where the signature insight becomes computable: **asks vs realised, one ledger.**

---

## 3.1 eBay Browse API ingestion

**Objective.** Official-ToS asks + sold + delisting behaviour at volume. First `enabled=1` row created under the Phase 0 G6 constraint — the constraint demonstrably works in the happy direction too.

**Tasks.**

1. Register eBay developer account; store key in Fly secrets. `sources` row: `id='ebay'`, `access_status='approved'`, `rights_basis='official_api_tos:<program-url>'`, `rights_reviewed_at=now`, `cadence_hours=24`.
2. Adapter `internal/sourcesv2/ebay`: Browse API `search` + `getItem` per family scope (Phase 2's five families only — scope discipline holds).
   - Active listings → observation `kind='ask'`.
   - `sold_items` where available → `kind='sold'` (eBay realised-adjacent; labelled distinctly from auction realised — **never mixed into auction tiers**).
   - Delist detection: nightly diff of item IDs seen; disappearing ID + last-seen price → `delist` event (append-only, derived by diffing, no mutation — G2).
3. Attribution fidelity: eBay category noise is high. Ingest only listings whose title resolves to a catalogue ref at cascade rung ≥2 (exact/structured) in v1; alias-resolved goes to review queue as usual.
4. Rate limits: respect official caps; store raw JSON in `raw_documents` like every source (G3).

**Acceptance.**

- 1,000 raw eBay responses stored; extractor fixtures byte-stable.
- Zero unresolved-ref rows auto-appended at confidence <0.85.

---

## 3.2 Derived signals (free because the ledger is append-only — PLAN §6)

**Objective.** The metrics nobody else surfaces, computed read-only from the observation ledger.

**Tasks.**

1. `internal/metrics` package, pure functions over observation sets:
   - `MedianDaysToDelist(obs)` — per reference; the real liquidity measure.
   - `PriceCutFrequency(obs)` — fraction of asks that dropped ≥1× before vanishing = mispricing signal.
   - `AskRealisedSpread(auctions, asks)` — median ask ÷ median auction-realised per reference, **published only where the realised side passes Phase 0 gates**.
2. Nightly read-model job computes per-reference metrics into a `reference_metrics` table (ref, week, days_to_delist, cut_freq, spread, counts) — pages render from this, never compute on request.
3. Every derived number carries: observation counts, window (trailing 365d), and ruleset hash. Same reproducibility harness applies — add these to the golden suite.

**Acceptance.**

- Spread for a reference recomputes identically from ledger via harness.
- Metrics absent (not zero) where data density insufficient — G9.

---

## 3.3 Reference page: the spread block

**Objective.** Ship the line that is the whole company (PLAN §3):

```
Asking prices        €14,200 – €16,800   (23 listings)
Auction realised     €12,900 – €14,100   (11 results)

Asks typically sit ~12% above realised for this reference.
```

**Tasks.**

1. New block on `/references/[ref]`, between realised and evidence blocks:
   - two labelled ranges (caliper renders both, colour-coded asks vs realised),
   - the spread sentence — templated, plain language, no "fair value" claims (editorial rule: no true-value language),
   - counts + window + "methodology" link.
2. Liquidity line: "median time to sell: N days (last 12 months)" where density allows.
3. **Guardrail:** spread publishes only when BOTH sides pass gates independently. A spread built on 3 auction results is a claim we refuse to make.
4. Phase 0 relabel completes here for covered families (F1): ask-only ranges leave the page.

**Acceptance.**

- Copy for a sample reference reviewed against editorial rules (no ROI, no true-value, no precision claims).
- Golden test: spread computation over frozen ledger fixture.

---

## Non-goals

- No eBay watch-list integration, no bidding anything.
- No non-coverage expansion beyond the five families (still holds).
- No per-listing verdicts from asks — asks remain context.

## Risks

| Risk | Mitigation |
|---|---|
| eBay category noise pollutes ledger | Rung-≥2 resolution gate for ingestion; review queue for the rest; per-week precision sample |
| Spread misread as "eBay overcharges" narrative | Copy: asks are seller aspirations platform-wide; the spread is market-structure fact, not a dig — it flatters nobody |
| Delist-diff false positives (bot-blocked fetches look like delists) | Delist only after N consecutive misses; fetch-failure runs excluded from diff windows |

## Exit criteria

- ≥1 of the five families publishes spread + liquidity on its references, both sides gate-clean, reproducible via harness.
- `reference_metrics` table populated for all five families where density allows.
