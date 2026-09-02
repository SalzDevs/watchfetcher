# Phase 7 — Licensing, Endgame & Category Expansion

> Status: **PLANNED (outline — detailed only when Phase 6 gate passes).**
> Source: PLAN.md §12 (stream 3), §18 (three outcomes), §16 (acquisition answer).
> This phase is mostly *not building*: it is convertibility — being shaped so that every
> good outcome (independence, acquisition at leverage, category expansion) is reachable
> without rework.

---

## 7.1 Licensed data — professional tier

**Objective.** Insurers, valuers, estate professionals subscribe to the ledger itself: accuracy + method + rights cleanliness. PLAN §18 outcome #1 — the independent reference layer.

**Product sketch (finalize after first real customer conversation).**

- Per-family ledger extracts: observations with provenance chain (source → raw doc → resolution confidence), gate status, ruleset hash. Signed files or read API.
- Licence terms: no resale, attribution required, no influence over method (same clause as dealers, again public).
- SLA posture: correctness over freshness — stale-but-honest beats fresh-but-wrong, stated in the SLA itself.

**Prerequisites (all earlier phases — this is why they were non-negotiable).**

- Rights evidence complete for every enabled source (Phase 0.2).
- Golden suite + reproduce harness demonstrable live in a sales call (0.4–0.5).
- Verified user-reported prices clearly tier-separated (Phase 5.4).

**Build trigger:** a named professional (insurer/valuer) has asked twice. Not before.

---

## 7.2 Acquisition-at-leverage readiness

**Objective.** If it happens: because we are valuable, not starving (PLAN §16). Concretely:

1. **The dataset they lack:** realised auction ledger + verified user-reported prices + rights-clean provenance. Keep these three first-class and portable (single Postgres/SQLite export, documented schema — the ledger *is* the company).
2. **The poison pill stays armed:** buyer-facing positioning ("we say don't buy") means post-acquisition value evaporates if they gut the honesty — this is a feature of our shape, not a defect to fix.
3. **Clean-rights dossier:** ongoing — a living document listing every source, its rights basis, its reviewer, its verification date. Assembled continuously by Phase 0 tooling; at negotiation it is a compiled artifact, not a scramble.
4. **No technical lock-in to Fly or any single vendor** that an acquirer would price as risk — infra is deliberately near-zero and boring.

**Annual ritual:** re-read PLAN §16; confirm nothing built in the year increases dependence on a single marketplace's goodwill.

---

## 7.3 Category expansion

**Objective.** The architecture is category-agnostic (PLAN §18 #3): append-only observation ledger + deterministic verdict engine + jurisdictional landed cost transfers to any opaque, high-value, cross-border secondary market.

**Transfer checklist (per candidate market — classic cars, cameras, guitars, art, watches-adjacent like pocket watches):**

1. Does a public realised-price record exist? (auction houses → usually yes)
2. Is comparability parameterisable by a curated catalogue of ≤200 SKUs/families? (yes for cameras/guitars; hard for art — art fails the catalogue test, skip)
3. Is there cross-border duty/VAT complexity? (landed cost engine reusability check)
4. Is the buyer community allergic to marketing? (distribution via free tool + communities reuses the Phase 1/11 playbook)

**Watches are the beachhead. Expansion starts only when watch-side Phases 0–5 run without the founder touching the engine weekly.**

---

## 7.4 The standing endgame dashboard (annual, one afternoon)

Re-answer, against current reality:

1. Are verdicts still 100% reproducible? (harness report)
2. Has any public number ever been changed by money? (change log review — expect: no)
3. Which outcome are we closest to: independent layer / acquisition / expansion?
4. What did we ship that the plan said not to? (cutlist)

If any answer is bad, the fix is subtractive.

---

## Done-ness definition for the whole plan

> **A stranger can paste a listing, see a defended range or an honest "not yet",
> get a landed cost with citations, and walk into a negotiation with our page as their evidence —
> and every number on it can be re-derived by anyone, from stored evidence plus a versioned ruleset, forever.**

That is WatchLedger. Everything in Phases 0–6 exists to make that sentence true and keep it true.
