# Phase 5 — Watchlist, Alerts, User-Reported Prices

> Status: **PLANNED.** Source: PLAN.md §4 (Tier 3 — proprietary data), §9, §11 (retention is outbound), §15 (the leading indicator).
> Depends on: Phase 4 (evaluations worth watching, trust banked). Duration: 2–3 weeks.
> Retention must be **outbound** — buyers transact 1–3×/year, nobody opens a dashboard monthly. And the deepest moat starts here: **prices users report themselves.**

---

## 5.1 Accounts (minimal)

**Objective.** Managed auth, nothing custom (PLAN §16 #7: buy auth, email, hosting — build only ledger, engine, rules).

**Tasks.**

1. Managed auth provider (e.g. hosted magic-link / OAuth) — email link only at launch. No passwords in our DB ever.
2. `users(id, email, created_at)` + session via provider SDK. No profiles, no usernames.
3. Middleware: watchlist/alerts/brief-save require auth; every other surface stays anonymous (trust posture: the evidence is public, the conveniences are personal).

**Acceptance.** Zero custom credential storage; session flow e2e test.

---

## 5.2 Watchlist

**Objective.** Save references, not products. The unit of attention is the reference (matches mental model + ledger).

**Tasks.**

1. `POST /api/watchlist {ref}` — from reference pages + evaluation permalinks.
2. Watchlist page: references with gate status, last range, spread (if covered), days since freshest comp — one row each, sorted by "activity since last visit".
3. Watchlist add buttons on: reference pages, evaluation results, brief pages.

**Acceptance.** Add-to-watchlist from an evaluation permalink ≤2 clicks from verdict.

---

## 5.3 Alerts (the retention loop)

**Objective.** Outbound value: something changed on a reference you flagged. Email only.

**Tasks.**

1. Alert triggers (each individually toggleable per watch):
   - new exact-tier realised comp appended (and gate state still pass),
   - gate state change (range appeared / range lost — "we now know / we no longer know" both honest, both valuable),
   - spread moved >10% since last alert,
   - liquidity shift (days-to-delist ±30%).
2. Nightly job: compute trigger set → batch email (managed provider) with deep links to reference pages. Frequency cap: max 1 email/user/week unless range-appeared (rare, allowed).
3. Every email footer: one-click watch removal + "why am I getting this" (the triggering rule, named).
4. Alert content = same read models as pages; no number in an email that isn't on the page.

**Acceptance.**

- Alert golden tests: trigger matrix (each rule fires / doesn't) on frozen ledger fixtures.
- Zero numbers in emails that fail the harness.

---

## 5.4 User-reported paid prices (the proprietary dataset)

**Objective.** The one data layer that cannot be bought or scraped (PLAN §4 Tier 3, §16 acquisition answer #1). Earned strictly through trust.

**Tasks.**

1. Report flow: from reference page ("bought this ref? tell us what you paid") — price, currency, date, condition-free (we don't grade), optional proof image (invoice screenshot) → `raw_documents` via upload pipeline.
2. Table `reported_prices(id, user_id, ref, price, currency, paid_at, proof_doc_id, status submitted|verified|rejected, reviewed_by, reviewed_at)`.
3. **Review before ledger.** Verified reports append to the ledger with `source_type='user_reported'`, distinct tier — they participate in *context*, and once volume ≥5 verified reports for a ref, in a **reported-price median line** on the reference page (labelled separately from auction realised; never merged into auction tiers).
4. Reviewer sees: user's prior evaluations of the same ref (corroboration signal), proof image, any contradictions (reported price wildly outside realised band → extra scrutiny, not auto-rejection — outliers are information).
5. Reporter sees: their report's status + a permanent "thank you — this made the ledger better" with a link to the line they contributed to. Compounding loop, visible.

**Acceptance.**

- No unverified report renders anywhere public, ever.
- Reviewer UI rejects in one click with reason; rejection is silent to other users.
- Proof images stored in raw store, access-controlled (PII care — invoices contain names).

---

## 5.5 Metrics wiring (PLAN §15)

**Objective.** Measure what the plan says matters; ignore what it says to ignore.

- Primary: **evaluations acted on** — brief generated, watchlist added, outcome reported. Instrument these three events only.
- Trust leading indicator: **verified user-reported prices submitted** per month.
- Supporting: references passing gates, alert open rate, corridor organic traffic (Search Console).
- Explicitly NOT instrumented: total refs tracked, total listings ingested (vanity — rewards breadth).

**Acceptance.** Dashboard query returns all five from DB/views; no analytics SDK on pages (privacy posture, speed).

---

## Non-goals

- Push notifications, SMS, in-app inbox.
- Social features, comments, sharing watches between users.
- Automatic import of user emails' purchase confirmations (creepy, fragile).

## Risks

| Risk | Mitigation |
|---|---|
| Nobody reports prices (cold start) | Make reporting a two-field form from an evaluation they just did (price already filled); ask at the moment of maximum trust — after a brief helped them |
| Alert fatigue → unsubscribes | Frequency cap + per-rule toggles + genuinely-rare triggers (range-appeared is the only uncapped one) |
| Fake reports pollute proprietary dataset | Proof image + review + band-contradiction scrutiny; dataset value depends on this gate more than any other |

## Exit criteria

- ≥100 references watched by real users; alert open rate ≥40% (cold email to warm intent).
- ≥10 verified reported prices in the ledger — the moat has started.
