# Phase 0 — Foundation: Ledger, Rulesets, Reproducibility

> Status: **PLANNED — approved for implementation, not started.**
> Source: PLAN.md §5, §6, §7, §13. Companion: [PHASES.md](../PHASES.md).
> Duration: 1–2 weeks. Depends on: nothing. Everything else depends on this.

---

## 0.1 Catalogue store

**Objective.** The curated taxonomy stops being a config file and becomes a versioned, aliased database store — the substrate for the resolver cascade (PLAN §7.1).

**Schema.**

```sql
CREATE TABLE catalogue_references (
  ref         TEXT PRIMARY KEY,          -- canonical, e.g. '126610LN'
  brand       TEXT NOT NULL,
  family      TEXT NOT NULL,
  dial        TEXT,                      -- '' = not pinned by SKU
  material    TEXT,
  aliases     jsonb NOT NULL DEFAULT '[]',
  version     INTEGER NOT NULL DEFAULT 1,
  updated_at  INTEGER NOT NULL
);

CREATE TABLE catalogue_aliases (
  alias     TEXT NOT NULL,               -- e.g. 'hulk', 'batman', 'panda'
  ref       TEXT NOT NULL REFERENCES catalogue_references(ref),
  kind      TEXT NOT NULL,               -- nickname | typo | market_name
  added_by  TEXT NOT NULL,               -- 'seed' | 'review_queue:<id>'
  created_at INTEGER NOT NULL,
  PRIMARY KEY (alias, ref)
);
```

**Tasks.**

1. Migration `0001_catalogue.sql`; seed from `config/models.json` (existing 102 refs) via `cmd/catalogue-seed`.
2. Generated Go taxonomy (`internal/attrs/references_gen.go`) becomes the **offline fallback**; runtime resolution reads DB, falls back to generated on DB absence (tests, CI).
3. Alias seed file `config/aliases.json` (hulk→126610LV, batman→126710BLNR, …) — curated, reviewed.
4. `attrs.LookupRef` gains alias rung: exact ref (0.98) → alias (0.90) → unresolved. No fuzzy in this phase.

**Acceptance.**

- Adding a reference or alias = one SQL insert, zero code changes.
- Resolver unit tests: alias hit returns ref with confidence 0.90 recorded on the resolution record.

---

## 0.2 Source registry — structural compliance (G6)

**Objective.** Make scraping-gated-inventory structurally impossible: a source cannot be enabled without recorded rights evidence.

**Schema.**

```sql
CREATE TABLE sources (
  id                TEXT PRIMARY KEY,   -- 'chrono24' | 'ebay' | 'phillips' | ...
  name              TEXT NOT NULL,
  access_status     TEXT NOT NULL DEFAULT 'unverified'
                    CHECK (access_status IN ('approved','pending','unverified','revoked')),
  rights_basis      TEXT,               -- 'public_record' | 'official_api_tos' | 'contract:2026-xxx'
  rights_reviewed_at INTEGER,
  reviewer          TEXT,
  cadence_hours     INTEGER,
  enabled           INTEGER NOT NULL DEFAULT 0,
  CHECK (enabled = 0 OR (access_status = 'approved'
         AND rights_basis IS NOT NULL AND rights_reviewed_at IS NOT NULL))
);
```

**Tasks.**

1. Migration `0002_sources.sql` + CHECK constraint exactly as above.
2. Seed current four marketplaces as `access_status='unverified', enabled=0` — historical data stays readable, no new fetches.
3. `internal/store.SourceEnabled(db, id) bool` — the ingest path's only on/off query.
4. CI test: migration on fresh DB → attempt `enabled=1` on an unverified source → must fail.

**Acceptance.** `UPDATE sources SET enabled=1 WHERE id='chrono24'` fails with constraint violation. The property lives in the schema, not in code review (PLAN §13).

---

## 0.3 Raw document store

**Objective.** Original source bytes retained forever, before any extraction (PLAN §6 "storage is cheap; re-collection may be impossible").

**Schema.**

```sql
CREATE TABLE raw_documents (
  id           INTEGER PRIMARY KEY,
  source_id    TEXT NOT NULL REFERENCES sources(id),
  url          TEXT NOT NULL,
  fetched_at   INTEGER NOT NULL,
  content_hash TEXT NOT NULL,          -- sha256 of body
  content_type TEXT NOT NULL,          -- 'text/html' | 'application/json' | ...
  body         BLOB NOT NULL,
  UNIQUE (content_hash)                -- dedup = change detection for free
);
-- No UPDATE path. Ever.
```

**Tasks.**

1. Migration `0003_raw_documents.sql`.
2. `internal/store.SaveRawDocument(sourceID, url, body) (hash, isNew, error)` — compute hash, INSERT OR IGNORE.
3. Reorder ingest: `fetch → SaveRawDocument → extract(bytes) → normalise → resolve → append observation`. Extractor signature becomes `Extract(raw []byte) ([]CandidateFact, error)` — pure, fixture-testable (G1).
4. Backfill: **none.** Past HTML is gone; accepted loss, documented. Accrual starts the day a rights-approved source is enabled.
5. `cmd/rawdump --source X --since` for debugging/auditing.

**Acceptance.** Fixture: store one real eBay JSON response; run extractor twice; identical CandidateFact sets; mutate extractor; facts change but raw bytes don't.

---

## 0.4 Rulesets + content-addressed verdicts (G4, G8)

**Objective.** Same evidence + same rules = same verdict, forever, re-derivable on demand.

**Schema.**

```sql
CREATE TABLE rulesets (
  id          INTEGER PRIMARY KEY,
  version     TEXT UNIQUE NOT NULL,    -- 'v1.0.0'
  hash        TEXT NOT NULL,           -- sha256 of canonical JSON definition
  definition  TEXT NOT NULL,           -- the full engine.Ruleset struct as JSON
  created_at  INTEGER NOT NULL
);

CREATE TABLE verdict_content (
  inputs_hash  TEXT NOT NULL,          -- engine.InputsHash: hash of evidence set + cell key
  ruleset_hash TEXT NOT NULL,
  cell_key     TEXT NOT NULL,
  result       TEXT NOT NULL,          -- full engine.Verdict JSON (minus receipts, which live in verdict_receipts)
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (inputs_hash, ruleset_hash)
);
```

**Code.**

1. `engine.Ruleset` struct + `Current` + `Hash()` — **partially implemented, uncommitted** (`internal/engine/engine.go`). Finish: move all statistical constants behind the struct; remove bare `const` block.
2. `engine.InputsHash(cellKey, inCell)` — order-independent sha256 over observation signatures (source, url, title, ref, price, timestamp). **Implemented, uncommitted.**
3. Engine writes both legacy `verdicts` table (UI compat) and `verdict_content` (audit spine) until Phase 2 flip point F1.
4. `cmd/reproduce --inputs-hash H --ruleset V`:
   - load evidence set (observations joined by the same cell + hash recomputation),
   - load ruleset definition by version,
   - recompute, byte-compare against `verdict_content.result`.
   - Exit 0 = reproducible; exit 1 with diff = trust incident.
5. Ruleset changes require: new `version` string + golden re-bless (0.5) + diff review. Same hash never overwrites.

**Acceptance.**

- For every verdict in prod: `cmd/reproduce` exits 0.
- Mutating any observation → different `inputs_hash` → different verdict row; the old one survives untouched.

---

## 0.5 Golden verdict suite

**Objective.** A silent shift in pricing logic is structurally impossible to ship unnoticed (PLAN §13).

**Tasks.**

1. `cmd/gengolden` — generates `internal/engine/golden.json`: frozen observation sets → frozen verdicts. **Skeleton written, uncommitted.**
2. Scenario matrix (minimum):
   - all four gates pass (healthy cell, 12 comps, 3 sources)
   - each gate failing **alone** (volume 6; single source ×12; all-stale; one fantasy ask inflating IQR)
   - below MinSamples → nil verdict
   - trim-band edge: cluster + outlier at exactly 2.2×
   - recency weighting: identical prices at different ages → different medians
3. `internal/engine/golden_test.go`: recompute every scenario with `Current` ruleset, deep-compare against `golden.json`. Mismatch = red build **unless** `golden.json` was deliberately regenerated with a ruleset version bump in the same PR.
4. CI guard: PR touching `internal/engine/*.go` without a `golden.json` change OR a `rulesets.version` bump → review-gate label, not silent merge.

**Acceptance.** Manual experiment: change `TrimHighMult` 2.2→2.5 without version bump → CI red with a per-scenario diff.

---

## 0.6 Eligibility gates + LIMITED COVERAGE surface

**Objective.** Publish ranges only when volume, diversity, freshness and dispersion all pass (PLAN §7.4). "We don't know yet" becomes a rendered, first-class output.

**Tasks.**

1. `engine.EvaluateGates(inCell, p25, p75, now) []failing` — **implemented, uncommitted.** Pure. Thresholds live in `Ruleset.Gates`.
2. `Verdict.GatesStatus` (`pass`/`limited`) + `FailingGates` — **implemented, uncommitted.**
3. API: `/api/verdict` response carries `gates_status`, `failing_gates`, `ruleset_hash`, `inputs_hash`. Optional filter `?gates=strict` → 404 when `limited`.
4. UI: verdict panel gains the LIMITED MARKET COVERAGE state — per-gate checklist with counts ("5 exact observations · 2 sources — needs 8 from 3"), replacing the generic "no verdict" copy when a cell exists but fails gates. The dead-end comps panel (already built) becomes the evidence body of this state.
5. Directory/`/api/verdicts`: rows carry `gates_status`; UI greys `limited` cells with reason on hover.

**Acceptance.** Golden scenarios from 0.5 map 1:1 to rendered states; a human can tell, for any cell, *why* there is or isn't a number.

---

## 0.7 Scrape freeze (G6 enforcement, flip point F1 pre-work)

**Objective.** Nightly pipeline stops fetching from rights-unverified sources. Historical data stays readable, relabeled honestly.

**Tasks.**

1. `scripts/run-nightly.sh`: fetch step gated on `SCRAPE_ENABLED` (default `0`). Engine + ingest still run (idempotent no-ops without new data) so existing nightly monitoring/alerting keeps working.
2. Dockerfile: fetcher binary stays (future rights-approved sources reuse the plumbing).
3. UI relabel, everywhere comp data renders: `ASKS · rights unverified · never an auction-realised input`. Visible in receipts panel footer + reference page source badges.
4. **Flip point F1 (documented, not executed):** the moment Phase 2 realised data covers a family, ask-derived ranges disappear from public surfaces for that family. Not before — PLAN §1's "we don't know yet" beats an empty product, but never a mislabelled number.

**Acceptance.** `SCRAPE_ENABLED=0 ./scripts/run-nightly.sh` → zero outbound requests (verify via crawl metrics file absent + network assertion in CI smoke).

---

## Risks & mitigations

| Risk | Mitigation |
|---|---|
| Gate thresholds (≥8/≥3/180d/2.5) gut current public verdict count | Gates ship as **reported status** in 0.6 (`pass`/`limited`), strict-enforcement flip is deliberate + dated (F1) |
| Ruleset struct churn breaks golden suite mid-development | Golden JSON regenerated only with version bump; generator + test land together |
| Raw store grows fast once sources enable | Bodies are HTML/JSON tens-of-KB; at 10⁵ docs/year ≈ GBs — Fly volume fine for years; add compression later, never deletion |
| Reproduce command needs evidence join | `inputs_hash` recomputation over cell observations; document that re-extraction differs from re-computation (only re-computation is guaranteed byte-equal) |

## Exit criteria (all of 0.2–0.7)

- `go test ./...` green including golden suite.
- `cmd/reproduce` exits 0 for 100% of prod verdicts.
- Nightly log shows zero scraping.
- Every published number on the site carries `ruleset_hash` in its API payload.
