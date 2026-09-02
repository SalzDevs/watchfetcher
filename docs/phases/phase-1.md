# Phase 1 — Landed-Cost Engine + Corridor Pages

> Status: **PLANNED.** Source: PLAN.md §8, §9, §11. Depends on: Phase 0 (G7 decimal money, rulesets for duty/VAT tables).
> Duration: ~2 weeks. This is the zero-data front door: real product, real SEO, no partnerships, no permission.

---

## 1.1 Tax-rule data model

**Objective.** Every line item cites its basis. A rule nobody can source cannot produce a number (PLAN §8 rule 2).

**Schema.**

```sql
CREATE TABLE tax_rules (
  id             INTEGER PRIMARY KEY,
  from_country   TEXT NOT NULL,        -- ISO-3166 alpha-2 ('JP')
  to_country     TEXT NOT NULL,        -- ('PT')
  hs_code        TEXT NOT NULL,        -- '9102.21' wrist-watches, electric
  duty_rate      TEXT NOT NULL,        -- decimal string '4.5'  (percent) — G7: no float
  vat_rate       TEXT NOT NULL,        -- decimal string '23'
  vat_basis      TEXT NOT NULL DEFAULT 'cif_plus_duty',
  insurance_pct  TEXT,                 -- optional, decimal string
  basis          TEXT NOT NULL,        -- human explanation of the rule
  source_url     TEXT NOT NULL,        -- official tariff / authority page
  verified_at    INTEGER NOT NULL,     -- unix
  verified_by    TEXT NOT NULL,
  ruleset_id     INTEGER NOT NULL REFERENCES rulesets(id),
  UNIQUE (from_country, to_country, hs_code, ruleset_id)
);

CREATE TABLE fx_rates (
  date   TEXT PRIMARY KEY,             -- 'YYYY-MM-DD' (ECB reference date)
  base   TEXT NOT NULL DEFAULT 'EUR',
  rates  TEXT NOT NULL                 -- jsonb {USD: 1.09, GBP: 0.85, ...}
);
```

**Tasks.**

1. Migration `0004_tax_rules.sql`, `0005_fx_rates.sql`.
2. Staleness wall: `verified_at` older than 180 days → UI renders staleness warning on every derived line; engine still computes (estimate framing) but flags (G9).
3. Seed script `cmd/ruleseed --file config/tax-rules.yaml`: human-edited YAML, one entry per corridor, each with source_url. Seed 10 launch corridors:
   - JP→PT, JP→DE, USA→UK, UK→USA, CH→DE, CH→FR, HK→DE, USA→PT, JP→UK, CH→IT
4. FX fetcher `cmd/fxfetch` — pull ECB daily reference XML → `fx_rates` (public data, cite ECB). Cron-able; manual run acceptable at launch.

**Acceptance.**

- Rendering a breakdown with a rule missing `source_url` or `verified_at` is impossible at the query layer (`GetActiveRule` returns `stale`/`unverified` states, never silently OK).
- Golden FX test: frozen ECB XML fixture → frozen converted amounts.

---

## 1.2 Engine (`internal/landedcost`)

**Objective.** Pure, deterministic, Decimal-only breakdown generator. Never touches the network (G1).

**API (Go).**

```go
type Input struct {
  ItemPrice     decimal.Decimal
  Currency      string        // source currency
  From, To      string        // country codes
  Shipping      decimal.Decimal // in destination currency, user-editable
  InsurancePct  decimal.Decimal // optional override
  MarginScheme  bool          // private-sale toggle (changes VAT expectation → warning path)
  FXDate        string        // 'latest' or pinned 'YYYY-MM-DD' (G8 reproducibility)
}

type LineItem struct {
  Label   string
  Amount  decimal.Decimal
  Basis   string   // citation: rule row + fx date + spread
  Stale   bool
}

type Result struct {
  Lines        []LineItem
  Total        decimal.Decimal
  OverAskPct   decimal.Decimal // vs original ask, when provided
  FXDate       string
  RulesetVersion string
  Warnings     []string        // margin-scheme caveat, staleness, private-import assumption
}

func Compute(in Input) (Result, error)
```

**Engine rules (PLAN §8, non-negotiable).**

1. `shopspring/decimal` everywhere; quantize (2 dp, banker's rounding documented) once at display serialization.
2. FX: `rate[from→EUR] × rate[EUR→to]` chained through EUR base; +1.5% card spread **stated as its own line**.
3. CIF = item + shipping + insurance. Duty = CIF × duty_rate. VAT = (CIF + duty) × vat_rate (configurable basis).
4. Currency mismatch (listing in USD, corridor USA→UK): FX-convert item price first, as its own cited line.
5. Margin scheme: when `MarginScheme=true`, VAT line is replaced by a warning line — "private margin sale: VAT typically settled at original import; verify with seller" — never a fabricated number.

**Acceptance.**

- Golden tests: 10 corridors × frozen FX fixture → frozen full breakdowns (same generator/bless flow as engine golden suite).
- Property test: reordering inputs never changes totals; every `LineItem.Basis` non-empty.

---

## 1.3 Corridor pages + calculator (web)

**Objective.** One canonical indexable page per corridor + the interactive tool. The shareable front door.

**Routes / components.**

- `apps/web/src/app/tools/landed-cost/[corridor]/page.tsx` — SSG. `corridor` = `japan-to-portugal`. Generates static params from the seeded corridor list.
- `apps/web/src/app/tools/landed-cost/page.tsx` — picker page (from/to/price) linking to canonical corridor.
- `src/components/LandedCostCalculator.tsx` — client: price, currency, shipping, insurance, margin toggle → calls `/api/landedcost`.
- Each corridor page: pre-rendered example breakdown (median corridor price), methodology summary, staleness badges, disclaimer footer, JSON-LD `FAQPage` + `SoftwareApplication`.

**API.**

- `POST /api/landedcost` `{item_price, currency, from, to, shipping?, insurance_pct?, margin_scheme?, fx_date?}` → `Result` JSON. Cache: `public, max-age=21600` unless `fx_date=latest` with pinned date.

**Content.**

- Copy per corridor: duty rate, VAT rate, worked example, "assumes commercial import" caveat. Generated from rule rows at build time — copy and rules can never disagree.

**Acceptance.**

- Lighthouse ≥95 (perf/SEO/a11y) on a corridor page.
- Every number on the page traceable to a `tax_rules` row + an `fx_rates` date.
- No login, no JS requirement for the static breakdown (progressive enhancement only for the interactive form).

---

## Non-goals (this phase)

- No duty optimisation advice, no de minimis handling beyond a stated warning line.
- No import from non-watch HS codes.
- No user accounts, no saving quotes.

## Risks

| Risk | Mitigation |
|---|---|
| Rule wrong → user under-declares | Prominent "estimate only — not customs advice" on every surface; per-rule source links; staleness wall |
| FX volatility changes totals between visits | Every result pins `fx_date`; permalink + `fx_date` re-derives exactly (G8) |
| Corridor pages cannibalised by thin content | Only ship corridors with verified rules + a real worked example |

## Exit criteria

- 10 corridor pages indexed in Search Console.
- One real forum/community use of the calculator observed or the corridor set is wrong (kill criterion for weak corridors, not the tool).
