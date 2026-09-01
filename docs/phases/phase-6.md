# Phase 6 — Dealer Portal + Revenue

> Status: **PLANNED.** Source: PLAN.md §11 (compounding sequence end), §12 (business model), §13 (the permanent statement), §16 (acquisition answer), §17 (second hire = BD).
> Depends on: Phase 5 (trust loop live, proprietary data accruing). Duration: open-ended — leverage-gated, not calendar-gated.
> Gate to start: reference pages earning consistent organic traffic AND ≥50 verified reported prices. **Attacking dealer feeds earlier is why this product usually dies (PLAN §10).**

---

## 6.0 The positioning (read before any sales call)

The pitch is not "pay us for data". The pitch is:

> "Our verdicts cannot be bought — stated publicly, permanently. What you can buy is knowing
> exactly where your inventory stands against the only realised-price ledger that admits its limits."

Revenue pays for the ledger's independence (PLAN §12: customers above pay for **accuracy**, not transactions). Affiliate commission remains rejected — permanently, not "at launch".

---

## 6.1 Dealer completeness audit (the wedge product)

**Objective.** A dealer's inventory vs the ledger: what's missing, mislabelled, or mispriced vs realised bands. They pay for the mirror, never for flattering.

**Tasks.**

1. Dealer uploads inventory file (CSV/sheet, manual — no integrations yet) → rows resolved via cascade (review queue where fuzzy).
2. Per-inventory report: resolution coverage %, refs absent from ledger (their exclusivity signal), condition-of-evidence per ref (gate status, counts, houses), positional stats vs realised bands (same positional language as Phase 4 — no verdicts).
3. Deliverable: the audit as a brief-style document (reuse Phase 4 PDF pipeline).
4. Sales-safety rule: audit reports are the dealer's private view; they never alter public pages. Stated on the portal's own about page.

**Acceptance.**

- Audit re-derives from ledger via harness (same as everything).
- Zero public-surface changes attributable to a paying dealer (CI: public page fixtures hash-stable across dealer onboarding).

---

## 6.2 Dealer subscriptions

**Objective.** Recurring revenue for standing access: monthly audit, competitive movement on watched families, ledger completeness trend.

**Tasks.**

1. Billing: managed provider (Stripe), plans: `audit` (one-off per audit) and `monitor` (monthly, N families).
2. Entitlements enforced at API layer by plan flags — no feature dimming of public data, ever (public = public, PLAN §13).
3. Contract template includes the independence clause verbatim from the methodology page.

**Acceptance.** A cancelled subscription changes nothing on any public page (test asserted).

---

## 6.3 Pro buyer tier

**Objective.** Power users pay for convenience and depth — not for correctness that stays free.

**Tasks.**

1. Free stays: all reference pages, all gate outputs, landed cost, N evaluations/month, weekly digest alerts.
2. Pro adds: unlimited evaluations, full personal history export (JSON + PDF briefs), saved corridor presets, spread alerts on all watched refs, early access to new families.
3. Pricing note: cheap. The moat is reported prices and rights position, not subscription revenue; price for volume of trust-building users.

**Acceptance.** Free tier contains every public number; export contains only data the user can already see.

---

## 6.4 Licensed data (Phase 7 seed — deliberately deferred)

**Objective.** Insurers, valuers, estate professionals pay for accuracy + method + rights cleanliness (PLAN §12, §18 outcome #1).

**Tasks (scoping only this phase — first real conversation gates the build).**

1. Licence product sketch: periodic ledger extracts per family with ruleset + provenance chain; API or signed files.
2. Prerequisite checklist (all Phase 0 deliverables, now revenue-relevant): rights evidence per source complete, golden suite green, reproduce harness demonstrated live in the sales call.
3. Do not build extracts until a named professional has asked twice.

**Acceptance (when triggered).** One signed data licence with rights appendix — the "rights-clean" asset is on the balance sheet as PLAN describes.

---

## 6.5 Second hire — business development (PLAN §17)

**Objective.** Dealer relationships are relationships. Founder stops doing BD the moment it works.

- Profile: watch-industry insider (ex-dealer, ex-auction house) who can explain the independence clause without marketing sauce.
- Comp: base + revenue share. No commission on anything transaction-adjacent — same poison-pill logic applied internally.

---

## Non-goals

- Marketplace features (listings, offers) — never.
- Selling to marketplaces *data that weakens buyer positioning* — the poison pill stands even at acquisition (PLAN §16).
- White-label verdicts — the verdict engine never becomes someone else's widget with their logo.

## Risks

| Risk | Mitigation |
|---|---|
| Dealers demand verdict influence → refuse → no revenue | Stated publicly pre-revenue (methodology page); the refusal IS the product's sales pitch; walk-away is cheap because infra is near-zero |
| Too-early BD burns credibility | Phase gate above: organic traffic + reported prices before first call |
| Audit perceived as surveillance by community | Audits are dealer-private; community-facing statement says so; no aggregated dealer rankings published |

## Exit criteria

- 3 paying dealers or 1 paying dealer + 100 pro subscribers (either proves willingness-to-pay for accuracy).
- Revenue covers infra + curator salary (PLAN §17 first hire) — independence is now self-funded.
