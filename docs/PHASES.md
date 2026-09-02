# WatchLedger — Execution Plan

> **DECISION (2026-08-31): PLAN.md is the single source of truth. No bridge, no middle path.**
> The marketplace scrapers are a stopped clock: frozen in Phase 0, relabeled, removed by Phase 2.
> **Flip point F1:** the public product shows no ranges derived from asks the moment realised
> data (Phase 2) can replace them. Until F1 the existing ask-derived ranges remain visible but
> flagged `UNVERIFIED RIGHTS · asks, not realised` — honest labelling, not endorsement.

> Operational companion to [PLAN.md](../PLAN.md). PLAN.md says **what** and **why**.
> This document says **in what order, in which files, with which acceptance criteria**.
> Each phase ends with something shippable. Each task has a definition of done.
> Nothing in a later phase may depend on an unfinished earlier phase except where stated.

---

## 0. Ground rules (read once, obey always)

These come straight from PLAN.md and are restated as engineering invariants:

| # | Invariant | Enforced by |
|---|---|---|
| G1 | Dependencies point downward: compute never touches the network; ingest never produces a verdict | Package layout (`internal/engine` has zero I/O imports; CI check) |
| G2 | Observations are append-only — a re-scrape never mutates a row | DB trigger or write-path convention + migration test |
| G3 | Raw source bytes are stored before any extraction | Ingest pipeline ordering |
| G4 | Verdicts are content-addressed: `hash(inputs) + hash(ruleset) → result` | Verdict write path |
| G5 | No range without passing all four eligibility gates | Engine gate function + golden tests |
| G6 | No scraping of gated inventory — a source is enabled only if `sources.enabled = true AND access_status='approved' AND rights_basis IS NOT NULL AND rights_reviewed_at IS NOT NULL` | DB constraint on `sources` table |
| G7 | Decimal arithmetic for money; float never crosses a money field | Go: `decimal.Decimal` / int64 minor units; TS: string-decimal lib |
| G8 | Every published number is re-derivable from stored evidence + ruleset version, forever | Reproducibility harness (Phase 0.4) |
| G9 | Degrade toward saying less: failures reduce published claims, never falsify them | Gate defaults + incident runbook |

**Existing watchfetcher assets — disposition:**

| Asset | Status under this plan |
|---|---|
| `internal/engine` (pure, exact-cell, trimmed range) | **Kept** — becomes the core of the range estimator, extended with gates |
| Append-only `observations` table | **Kept** — migrated into Observation ledger v2 |
| Curated taxonomy (`config/models.json` + generated `internal/attrs`) | **Kept** — becomes the Catalogue store's seed |
| Marketplace scrapers (`internal/sources/*`, `cmd/fetcher`) | **Frozen at end of Phase 0.** Data relabeled `ASKS · UNVERIFIED RIGHTS`, kept in the ledger for ask-vs-realised comparison but never a verdict input. No new sources added under G6. Delete in Phase 2 when auction data replaces it |
| Vault picker UI (`apps/web`) | **Kept** — becomes the Reference page surface (Phase 2) |
| Verdict history table | **Kept** — subsumed by content-addressed verdicts |
| Landed cost, evaluator, briefs, alerts | **New** — Phases 1, 4, 5 |

---

## Phase 0 — Foundation: ledger, rulesets, reproducibility

**Goal:** every invariant above is true in code and CI before a single new feature ships.
**Duration:** ~1–2 weeks. **Depends on:** nothing.

### 0.1 Catalogue store
- [ ] Promote `config/models.json` from config file to database table `catalogue_references` (`ref PK, brand, family, dial, material, aliases jsonb, version`).
- [ ] Alias table: `aliases(alias, ref, added_by, source)` — seeds the resolver cascade (PLAN §7.1).
- [ ] Generated Go taxonomy now reads from DB at build/boot; keep generated static fallback for tests.
- [ ] Acceptance: adding a reference + alias requires one row, no code change; resolver resolves alias → ref at 0.90 confidence.

### 0.2 Source registry with structural compliance (G6)
- [ ] Table `sources(id, name, access_status, rights_basis, rights_reviewed_at, reviewer, cadence, enabled)`.
- [ ] DB CHECK constraint: `enabled = false OR (access_status='approved' AND rights_basis IS NOT NULL AND rights_reviewed_at IS NOT NULL)`.
- [ ] Migrate current four marketplaces in as `access_status='unverified', enabled=false` for reads of historical data only.
- [ ] Acceptance: `UPDATE sources SET enabled=true` without rights columns fails at the DB level.

### 0.3 Raw document store
- [ ] Table `raw_documents(id, source_id, fetched_at, url, content_hash, body bytes, content_type)`. Immutable: no UPDATE grants; dedup on `content_hash`.
- [ ] Ingest pipeline order becomes fetch → store raw → extract → normalise → resolve → append observation.
- [ ] Backfill: none (past HTML is gone — accepted loss; starts accruing now).
- [ ] Acceptance: re-running an extractor against a stored document reproduces the same facts (fixture test).

### 0.4 Verdict reproducibility harness (G4, G8)
- [ ] `rulesets(id, version, hash, definition jsonb, created_at)` — every statistical rule (trim band, half-life, gates, adjustments) lives here, versioned.
- [ ] Verdict write: `verdicts(id, inputs_hash, ruleset_hash, result jsonb, created_at)`. Lookup by `(inputs_hash, ruleset_hash)`.
- [ ] Harness command: `go run ./cmd/reproduce --verdict <id>` → recomputes from ledger + ruleset, asserts byte-equal result.
- [ ] Acceptance: any stored verdict re-derives exactly, months later, from evidence + ruleset.

### 0.5 Golden verdict test suite
- [ ] Freeze 20–50 observation sets spanning: pass-all-gates, fail-each-gate-individually, too-few-sources, stale, high-dispersion.
- [ ] CI: recompute each against the pinned ruleset; any diff fails the build.
- [ ] Acceptance: changing trim band or gate thresholds without bumping ruleset version → red build.

### 0.6 Eligibility gates (PLAN §7.4)
- [ ] Engine gains gate evaluation: volume ≥8, sources ≥3, freshest <180d, IQR ratio <2.5.
- [ ] Response shape: `{status: "range" | "limited", range?, evidence: {count, sources, freshest, iqr_ratio}, failing_gates[]}`.
- [ ] UI: "LIMITED MARKET COVERAGE" panel with per-gate status (extend the existing dead-end comps panel).
- [ ] Acceptance: golden suite covers each gate failing alone.

### 0.7 Sunset scrape pipeline (G6)
- [ ] `cmd/fetcher` removed from nightly (`scripts/run-nightly.sh` → ingest only if sources enabled; with all disabled, nightly becomes no-op).
- [ ] Existing asks data stays queryable, relabeled in UI: `ASKS · source rights unverified · never a verdict input`.
- [ ] Acceptance: nightly log shows zero outbound scraping; historical data intact.

**Phase exit criteria:** G1–G9 all demonstrably true; golden suite green in CI; prod nightly does no scraping.

---

## Phase 1 — Landed-cost engine + corridor pages

**Goal:** the zero-data front door. Real product, real SEO, no dependency on Phases 2+.
**Duration:** ~2 weeks. **Depends on:** Phase 0 (decimal money G7, rulesets for duty/VAT tables).

### 1.1 Rules data model
- [ ] Table `tax_rules(id, from_country, to_country, hs_code, duty_rate, vat_rate, basis text, source_url, verified_at, ruleset_id)`.
- [ ] Staleness rule: `verified_at` older than 180 days → UI warning, auto (G9).
- [ ] Seed 10 high-traffic corridors (JP→PT, USA→UK, CH→DE, HK→EU, UK→USA, …) with cited sources (official tariff pages).
- [ ] Acceptance: every rule row has `source_url` + `verified_at`; a rule without them cannot render a number.

### 1.2 Engine
- [ ] `internal/landedcost` — pure, Decimal-only: FX (ECB daily rate + stated card spread), insurance, CIF, duty, VAT on (CIF+duty).
- [ ] Output = line-item breakdown, each with basis citation; assumptions editable (shipping cost, margin scheme toggle).
- [ ] Embeds `fx_date` + `ruleset_version` in every response (G8).
- [ ] Acceptance: golden tests per corridor with frozen FX; float never touches money (lint rule or code review checklist).

### 1.3 Corridor pages + calculator UI
- [ ] Route `/tools/landed-cost/[from]-to-[to]` — SSG, one page per corridor, canonical URL, structured data.
- [ ] Interactive calculator: price + currency + corridor → breakdown; manual-entry form is first-class (PLAN §9).
- [ ] Disclaimer block: estimate only, not customs advice.
- [ ] Acceptance: Lighthouse ≥95 on corridor page; every number on page traceable to a rule row.

**Phase exit criteria:** corridor pages indexed, calculator answers a real import query end-to-end with cited rules.

---

## Phase 2 — Auction ingestion + reference pages

**Goal:** the credibility layer. Realised prices from public auction records; the reference page as permanent SEO body.
**Duration:** 3–5 weeks. **Depends on:** Phase 0 (ledger, resolver), benefits from Phase 1 traffic.

### 2.1 Source adapters (rights-approved public records)
- [ ] Adapter interface: `internal/sources` v2 — fetch (rate-limited) → raw store → extractor.
- [ ] Start: Phillips, Christie's, Sotheby's **published results pages** (public record). One adapter per house; per-house parsers as fixtures.
- [ ] Extraction targets: lot ref (resolver cascade), sale date, hammer price + premium, currency, exact-tier variants stated in catalogue notes.
- [ ] Acceptance: 20 stored lots per house re-extract identically from raw bytes (fixture tests).

### 2.2 Reference resolver in production
- [ ] Cascade: structured field → exact normalised ref → alias → (fuzzy ≥0.70 → review queue, never auto-accept) → unresolved.
- [ ] Review queue table + minimal admin UI; every human resolution writes an alias.
- [ ] Acceptance: precision ≥0.85 on the auto-accepted rungs measured over 200 sampled lots.

### 2.3 Reference pages (the SEO body)
- [ ] Route `/references/[ref]`: the existing Vault picker becomes navigation; page shows:
  - auction realised range (exact tier only), counts, source diversity, gate status
  - ask-vs-realised comparison **once Phase 3 lands** (before that: asks hidden entirely)
  - visible exclusions with reasons (PLAN §7.2)
- [ ] Scope discipline: **five families only** — Submariner, Datejust, Speedmaster Professional, Black Bay, SKX/Alpinist. Everything else renders "not yet covered" (PLAN §10).
- [ ] Acceptance: every number on a reference page re-derives via the harness (0.4).

### 2.4 Methodology page
- [ ] Public, versioned, dated. Ruleset definitions rendered read-only from `rulesets` table.
- [ ] Acceptance: methodology text and shipped ruleset never disagree (generated from same source).

**Phase exit criteria:** 5 families × ~20 references each have live pages backed by ≥100 stored auction lots, all passing the reproducibility harness.

---

## Phase 3 — eBay API: asks vs realised

**Goal:** the signature insight — asks vs realised spread, liquidity signals. First officially-sanctioned data source.
**Duration:** 2–3 weeks. **Depends on:** Phase 2 (realised side of the comparison).

- [ ] eBay Browse API (free key, official ToS) → marketplace observations with `access_status='approved'` — first row enabled under the G6 constraint.
- [ ] Sold/delist tracking: append-only observations give delist analysis free (PLAN §6 derived signals).
- [ ] Derived metrics per reference: median days-to-delist, price-cut frequency, ask-vs-realised spread (published only where gate-passing realised data exists).
- [ ] Reference page gains the spread block (PLAN §3 "the line that is the whole company").
- [ ] Acceptance: spread for a reference recomputes identically from ledger; liquidity metrics present where data density allows.

**Phase exit criteria:** at least one reference page publishes asks-vs-realised spread backed by two right-cleared sources.

---

## Phase 4 — Listing evaluator + negotiation brief

**Goal:** the core job. Paste a listing URL → verdict + landed cost → shareable evidence PDF.
**Duration:** 3 weeks. **Depends on:** Phases 1–3.

- [ ] URL paste → fetch listing (approved sources only; unsupported source → structured manual form, which is first-class per PLAN §9).
- [ ] Resolver cascade runs on title/description; low confidence → "we resolved this to X — confirm?" (human-in-loop UI feeding alias table).
- [ ] Output: verdict panel (existing Caliper UI reused) + landed-cost block + evidence bundle (comparables with tiers, exclusions with reasons, ruleset version).
- [ ] Negotiation brief: one-page PDF — evidence, spread, landed cost, methodology footer. The share vector.
- [ ] Acceptance: evaluation permalink re-derives byte-for-byte (harness); brief contains zero unverifiable claims.

**Phase exit criteria:** a user can paste a real listing and walk into a negotiation with our PDF.

---

## Phase 5 — Watchlist, alerts, user-reported prices

**Goal:** retention loop + the proprietary dataset.
**Duration:** 2–3 weeks. **Depends on:** Phase 4 (evaluations worth watching).

- [ ] Auth (managed provider — no custom auth per PLAN §16 #7).
- [ ] Watchlist on reference pages; alerts on gate-passing events (new comps, spread moves, gate state changes).
- [ ] User-reported paid prices: structured form + optional proof image → raw store → review queue → ledger with `source_type='user_reported'`, confidence tier distinct.
- [ ] Acceptance: reported price enters ledger only after review; count of reports becomes a first-class trust metric.

**Phase exit criteria:** first proprietary paid-price observations in the ledger.

---

## Phase 6 — Dealer portal + revenue

**Goal:** leverage phase. Traffic + rights-clean position → dealer conversations.
**Duration:** open-ended. **Depends on:** Phase 5 trust loop.

- [ ] Dealer completeness audit (what they list vs ledger reality), positioning view.
- [ ] Pro buyer tier: unlimited alerts, history export, brief generation.
- [ ] Public, permanent statement: dealers can never pay to change a verdict.
- [ ] Acceptance: revenue contract signed on the "we never earn when you buy" pitch — the only sales pitch that survives the plan's ethics section.

---

## Cross-cutting workstreams (continuous)

| Workstream | Cadence |
|---|---|
| Tax-rule verification sweep (Phase 1 rules) | Monthly, 180d staleness wall |
| Review queue triage (aliases, reported prices) | Weekly |
| Golden suite maintenance: new rules → new ruleset version → suite updated deliberately | Per release |
| Metrics: evaluations acted on, references passing gates, reported prices submitted | Weekly dashboard |
| Logo/image rights review on public assets | Before any paid acquisition conversation |

---

## Immediate next actions (this week)

1. **Phase 0.2** — source registry + DB constraint (small, unblocks the ethics invariant, forces the scraper decision into the open).
2. **Phase 0.4 + 0.5** — rulesets table + golden suite harness (the spine everything hangs on).
3. **Phase 0.6** — gates in engine + LIMITED COVERAGE panel (reuses the dead-end comps UI).
4. **Phase 1.1–1.3** — landed cost engine + 3 corridors live (ship something the plan calls the front door).
5. Freeze `cmd/fetcher` from nightly (0.7) once 0.2 lands.
