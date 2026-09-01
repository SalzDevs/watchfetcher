# Phase 4 — Listing Evaluator + Negotiation Brief

> Status: **PLANNED.** Source: PLAN.md §3 (the pivot), §7, §9, §11 (share vector).
> Depends on: Phases 1–3. Duration: ~3 weeks.
> The core job: **paste a link → is this price fair, what will it really cost me, show me why.**

---

## 4.1 Listing ingestion (evaluator input)

**Objective.** Listing URL → structured candidate → resolved reference → verdict context. Manual entry is the first-class sibling, never the degraded fallback (PLAN §9).

**Tasks.**

1. `POST /api/evaluate {url | manual_fields}`.
   - URL path: fetch only **rights-approved** sources' public listing pages (source registry check first — G6 in the request path). Unsupported domain → 302 into manual form with prefilled title, no error shaming.
   - Raw bytes → `raw_documents` (G3) before any parsing.
   - Extractor per approved domain; extract price, currency, condition text, box/papers claims, ref, title.
2. **Manual form** (`/evaluate`): brand, family, ref (vault picker), price, currency, scope, year, condition notes. Same downstream pipeline as parsed listings. At launch, majority path — design it beautiful.
3. Resolver cascade runs on title/description. Sub-0.85 confidence → "We resolved this to **X (78% match)** — confirm or correct?" — the confirm/correct writes alias rows (human-in-loop UI = compounding precision, PLAN §7.1).
4. Ephemeral evaluations are **not** appended to the public ledger (a single viewed listing is not an observation). Saved evaluations (Phase 5 watchlist) may become observations after review. State this on the page.

**Acceptance.**

- Paste-to-result ≤5s for approved sources.
- Every evaluation permalink carries `inputs_hash` + `ruleset_hash`; `cmd/reproduce` extends to evaluations (re-derive from stored raw + cascade decision record).

---

## 4.2 Verdict composition

**Objective.** One page: the range, the landed cost, the evidence, the exclusions, the citations.

**Composition.**

1. Realised range for the resolved ref (Phase 2 read-model) — exact tier only.
2. Where the listing sits vs range: within / above p90 / below p10 — **positional language only** ("asks 9% above the top of the realised band"), never "overpriced"/"underpriced" verdicts (editorial rules: no true-value claims).
3. Landed cost: pre-filled from listing price + listing country guess → corridor engine (Phase 1), assumptions surfaced and editable (PLAN §8 rule 3).
4. Evidence bundle: comparables table with tiers (EXACT only for the range; VARIANT/RELATED labelled context; EXCLUDED with reasons), gate status, spread (where Phase 3 covers the family), ruleset + FX hashes.
5. Gate-fail path: the LIMITED MARKET COVERAGE panel + the evidence that exists — same honesty, no number (PLAN §7.4).

**Tasks.**

1. `internal/evaluate` orchestration package: resolve → fetch verdict read-model → landed cost → bundle. Pure composition over read models.
2. UI: `apps/web/src/app/evaluate/page.tsx` (form) + `/evaluate/[id]/page.tsx` (permalink). Reuse Caliper, badges, dead-end panel.
3. Evaluation permalinks stored in `evaluations(id, inputs_hash, listing_hash, result jsonb, created_at)` — content-addressed like everything else (G4).

**Acceptance.**

- Any evaluation permalink re-derives byte-for-byte.
- An evaluation of an unsupported/ambiguous listing degrades to manual + counts, never a fabricated range.

---

## 4.3 Negotiation brief (the share vector)

**Objective.** One-page PDF a buyer takes into a negotiation. Useful to the user first; every share is organic distribution (PLAN §11 mechanism 2).

**Tasks.**

1. Server-rendered print CSS page (`/evaluate/[id]/brief`) → user prints/saves as PDF (no PDF-generation service — near-zero infra posture, PLAN §12).
2. Brief contents, fixed order:
   - Reference + resolved identity (with confirmation badge if human-confirmed),
   - Auction realised range + counts + houses + window,
   - Ask vs realised spread (where covered),
   - The listing's position (positional language only),
   - Landed-cost breakdown to user's country (editable inline, re-renders),
   - Exclusions and what they mean,
   - Methodology + ruleset hash footer: "re-derive this page: instructions at /methodology".
3. Editorial rules enforced in a template test: no "undervalued", no "bargain", no "fair price" — position + evidence only.

**Acceptance.**

- Brief fits one A4 page at default settings.
- Template test greps rendered HTML for banned phrases → CI.

---

## Non-goals

- No Chrome extension, no auto-refresh monitoring (Phase 5).
- No condition grading, no authenticity commentary — excluded by editorial rule.
- No support for non-approved marketplaces beyond manual entry.

## Risks

| Risk | Mitigation |
|---|---|
| Listing pages are JS-walled (fetch gets shell) | Manual form prefilled from URL slug; degrade gracefully, log domain coverage gaps for future rights conversations |
| Users treat positional language as verdict anyway | Brief title: "Evidence sheet — [ref]"; position sentence phrased vs the *band*, not the watch |
| Landed cost wrong corridor guess | Corridor selector prominent on brief; default from listing TLD, always editable |

## Exit criteria

- 25 evaluations run by real users (seed via Phase 1/2 traffic), ≥5 briefs generated/shared externally.
- Zero editorial-rule violations in rendered briefs (CI-guarded).
- Reproduce harness covers evaluations end-to-end.
