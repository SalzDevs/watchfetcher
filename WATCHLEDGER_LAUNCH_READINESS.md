# WatchLedger — Launch Readiness

**Grounded in `SalzDevs/watchfetcher@main`.** Every claim below cites a file. This
supersedes the previous version of this document, which was written without reading
the code and was wrong about roughly half of what it asserted.

---

## 0. What changed, and why this rewrite exists

The previous launch readiness plan assumed a greenfield repo. The actual state:

| Previous claim | Reality in the repo |
|---|---|
| "Tax rules don't exist" | `internal/store/migrations/0004_tax_rules.sql` — schema exists; `0005_seed_corridors.sql` seeds **10 corridors** |
| "3 days to build the schema" | Built. The problem is the *contents*, not the schema |
| "Use `shopspring/decimal`" | Already used, wrapped in `internal/money/money.go` |
| "Store rates as `rate_bps INTEGER`" | Stored as TEXT decimal strings. Different, and fine. Leave it |
| "No deployment" | `fly.toml` → app `watchfairvalue`, region `lhr`, live |
| "Domain not registered" | `watchfairvalue.com`, Search Console verified (`internal/httpx/static/google317ab87a292b5275.html`) |
| "No email infrastructure" | `internal/auth/resend.go` + `internal/auth/smtp.go` + a `Mailer` interface |
| "No accounts; Phase 1 has no logged-in state" | `0009_accounts.sql`, `internal/auth/auth.go`, magic links, sessions, watchlist — all shipped |
| "No analytics / no sitemap" | `handleSitemap`, `handleRobots` in `internal/httpx/server.go` |

**The build is not the risk.** The product is at roughly Phase 4–5 of its own
`docs/PHASES.md`, is deployed, and is indexable. The risk has inverted:

> **WatchLedger is currently publishing unverified numbers with official-looking
> citations beside them, on a live indexed site, with no backups.**

That is the launch blocker. Everything else is secondary.

---

## 1. 🔴 BLOCKER — The site publishes unverified tax rates as verified

### The evidence

`0005_seed_corridors.sql` inserts ten corridors. Every row ends:

```sql
strftime('%s','now'), 'seed', 'v1.0.0'
```

`verified_by = 'seed'`. `verified_at = whenever the migration ran`.

Now trace what the site does with that:

- `internal/httpx/server.go` → `handleCorridor` renders
  `"VerifiedAt": rule.VerifiedAt.Format("2006-01-02")`.
- `internal/httpx/templates/tools_pages.html` prints:
  `Rule verified {{.VerifiedAt}}. Source: <a href="{{.SourceURL}}">official tariff</a>.`
- `templates/index.html` → `methodology.html` prints a table of every rule with a
  `verified` column and a `tariff ↗` link.

A visitor sees *"Rule verified 2026-09-08. Source: official tariff"* next to a number
nobody checked.

### The part that makes it worse

The 180-day staleness wall (`landedcost.MaxRuleAgeDays`) is measured from
`verified_at`, which the seed set to *now*. So the freshness mechanism — the thing
built specifically to stop stale numbers being published — **certifies the unverified
seed data as fresh for 180 days.** The safety mechanism launders the exact problem it
exists to catch.

### The part that is unarguable

`templates/tools_pages.html`, in `corridor_missing.html`:

> "We only publish corridors whose tax rules are verified against an official source."

That sentence is currently false, in the product's own words, on the product's own
page.

### Two rates that are probably wrong, not merely unverified

**JP → UK, duty `0`**, basis text: *"UK import duty 0% for used wrist-watches under
preference rules (verify origin)"*.

UK–Japan CEPA preference requires the goods to **originate in Japan**, with a statement
on origin. A Rolex bought from a Tokyo dealer is of **Swiss** origin. It is
Japanese-*located*, not Japanese-*originating*. Preference does not apply; the UK
Global Tariff does. The parenthetical `(verify origin)` is the author admitting the
rate is conditional — and then shipping it as unconditional.

This understates the bill for the single most common journey the product exists to
price.

**US → UK, duty `0`**, same basis text. There is no US–UK free trade agreement. 0% is
almost certainly wrong.

### Fix — ship this before anything else

**1. Add a publication gate to the schema.**

```sql
-- 0011_rule_publication_gate.sql
ALTER TABLE tax_rules ADD COLUMN source_quote TEXT NOT NULL DEFAULT '';
ALTER TABLE tax_rules ADD COLUMN duty_basis   TEXT NOT NULL DEFAULT 'cif';

-- seed rows are provisional until a human re-verifies them
UPDATE tax_rules SET verified_by = 'unverified' WHERE verified_by = 'seed';
```

**2. Refuse to serve unverified rules.** `internal/landedcost/store.go`, `LoadRule`
currently does `ORDER BY id DESC LIMIT 1` with no filter:

```go
func LoadRule(db *sql.DB, from, to string) (Rule, error) {
	var r Rule
	var verifiedAt int64
	err := db.QueryRow(`
		SELECT from_country, to_country, hs_code, duty_rate, vat_rate, vat_basis,
		       insurance_pct, basis, source_url, verified_at, verified_by, ruleset_version
		FROM tax_rules
		WHERE from_country = ? AND to_country = ?
		  AND verified_by NOT IN ('seed','unverified')
		  AND source_quote <> ''
		ORDER BY verified_at DESC LIMIT 1`, from, to).Scan( /* ... */ )
	if err == sql.ErrNoRows {
		return Rule{}, fmt.Errorf("no verified rule for corridor %s-%s", from, to)
	}
	// ...
}
```

`handleCorridor` already renders `corridor_missing.html` on a `LoadRule` error, and
that page already says the right thing. The gate therefore degrades correctly with no
new UI: an unverified corridor becomes *"We don't cover this corridor yet."*

**3. Keep unverified corridors out of the sitemap.** `handleSitemap` and
`handleToolsIndex` both call `ListCorridors`, which has no filter. Add the same
`verified_by` condition there — otherwise Google indexes pages that render "not
covered".

**4. Verify two corridors by hand, properly.** Not ten. Two.

The verification record for each is: the exact tariff line, the date read, and a
verbatim quote pasted into `source_quote`.

```sql
INSERT INTO tax_rules
  (from_country, to_country, hs_code, duty_rate, vat_rate, vat_basis, duty_basis,
   insurance_pct, basis, source_quote, source_url,
   verified_at, verified_by, ruleset_version)
VALUES
  ('JP','PT','9102.21','4.5','23','cif_plus_duty','cif','0.5',
   'EU TARIC third-country duty 4.5% on HS 9102 21 00; Portugal standard VAT 23% on CIF+duty',
   'Erga omnes third country duty: 4.5 % — TARIC measure for 9102 21 00 00',
   'https://ec.europa.eu/taxation_customs/dds2/taric/measures.jsp?Lang=en&Taric=9102210000',
   strftime('%s','now'), 'rceia', 'v2026.09.1');
```

**Launch with Japan → Portugal and Japan → Germany only.** Both are EU, both use the
same TARIC duty line, and both are the highest-intent corridors in the GTM plan. Gate
the other eight until each earns a `source_quote`.

Do **not** launch JP→UK or US→UK until the origin question is resolved in writing.

---

## 2. 🔴 BLOCKER — There are no backups

`scripts/prod-start.sh` starts the web server and a nightly loop. There is no
Litestream process. There is no `litestream.yml` in the repo. `fly.toml` mounts one
volume:

```toml
[mounts]
  source = "watchledger_data"
  destination = "/data"
```

`.gitignore` excludes `data/`. Decision D2 says "SQLite on Fly volume + Litestream."
Litestream was never wired up.

The entire product is an append-only evidence ledger whose value is that it is
permanent. It currently lives on exactly one unreplicated volume.

### Fix

`litestream.yml`:

```yaml
dbs:
  - path: /data/watchledger.sqlite
    replicas:
      - type: s3
        bucket: ${LITESTREAM_BUCKET}
        path: watchledger
        endpoint: ${LITESTREAM_ENDPOINT}
        access-key-id: ${LITESTREAM_ACCESS_KEY_ID}
        secret-access-key: ${LITESTREAM_SECRET_ACCESS_KEY}
        retention: 720h
        snapshot-interval: 6h
```

`scripts/prod-start.sh`, before the web server starts:

```sh
if [ -n "${LITESTREAM_BUCKET:-}" ]; then
  if [ ! -f "$DB" ]; then
    echo "[prod-start] no local db — restoring from replica"
    litestream restore -if-replica-exists -config /etc/litestream.yml "$DB" \
      || echo "[prod-start] restore failed — starting empty"
  fi
  echo "[prod-start] litestream replicate"
  exec litestream replicate -config /etc/litestream.yml -exec \
    "/usr/local/bin/watchledger-web --db $DB --addr :$PORT"
fi
```

This changes the process model: Litestream becomes PID 1 and supervises the web server.
Move the nightly work to a separate Machine or a `fly.toml` process group rather than
backgrounding it in the same container. The current loop —

```sh
sleep 90            # let boot settle
while true; do nightly; sleep 86400; done
```

— silently never runs a nightly if the machine restarts more often than once a day,
which it will during deploys. The schedule also drifts by the duration of each run.

**Then test the restore.** A backup you have not restored from is not a backup.
Restore into a scratch volume, boot it, and confirm `/api/meta/stats` returns the same
observation count.

---

## 3. 🔴 BLOCKER — `POST /login` is an unauthenticated email cannon

`internal/httpx/server.go`:

```go
mux.HandleFunc("POST /login", s.handleLoginStart)
```

`handleLoginStart` → `auth.StartLogin` → `mailer.SendLoginLink`. There is **no rate
limiting anywhere in the codebase** — no limiter, no per-IP counter, no CAPTCHA, no
delay. The only middleware in `Routes()` is `withLogging`.

`internal/auth/resend.go`, first line: *"free tier (3,000 emails/month, 100/day)."*

So: 100 curl requests kills login for the day. A few thousand kills the month, and
sends unsolicited mail from your domain to arbitrary addresses, destroying the sending
reputation the alerts product depends on.

The same absence applies to `POST /reports/submit` and `POST /watchlist/add` — both
authenticated, both unbounded, and every watchlist row is a future alert email.

### Fix

```go
// internal/httpx/limit.go
package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter — token bucket per key. In-process is sufficient: one machine.
type limiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     float64 // tokens per second
	capacity float64
}

func newLimiter(perMinute, burst float64) *limiter {
	return &limiter{buckets: map[string]*bucket{}, rate: perMinute / 60, capacity: burst}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.capacity, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.capacity {
		b.tokens = l.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func clientIP(r *http.Request) string {
	if f := r.Header.Get("Fly-Client-IP"); f != "" {
		return f
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *limiter) throttle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests — wait a minute and try again",
				http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
```

Wire it in `Routes()`:

```go
loginLimit := newLimiter(3, 3)   // 3/min per IP
writeLimit := newLimiter(20, 20)
apiLimit   := newLimiter(60, 60)

mux.HandleFunc("POST /login", loginLimit.throttle(s.handleLoginStart))
mux.HandleFunc("POST /evaluate", writeLimit.throttle(s.handleEvaluateSubmit))
mux.HandleFunc("POST /reports/submit", writeLimit.throttle(s.requireUser(s.handleReportSubmit)))
mux.HandleFunc("POST /watchlist/add", writeLimit.throttle(s.requireUser(s.handleWatchAdd)))
mux.HandleFunc("GET /api/landedcost", apiLimit.throttle(s.handleLandedCostAPI))
```

Add a second, global cap on outbound mail so a bug cannot drain the quota either: a
`sent_emails(day TEXT PRIMARY KEY, n INTEGER)` row checked in `auth.StartLogin` and
`alerts.Run`, refusing above ~80/day.

---

## 4. 🔴 BLOCKER — `POST /ebay/notifications` accepts unbounded unauthenticated writes

Two defects compound in `internal/httpx/server.go`:

```go
func ioReadAll(r *http.Request) ([]byte, error) {
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}
```

The `len(body) > 10<<20` check in `handleEbayDeletion` happens **after** the whole body
is already in memory. The cap is not a cap.

The handler's own comment concedes the second defect:

> "JWS signature verification with eBay's public cert = hardening TODO"

So any anonymous caller can POST arbitrary bytes and have them `INSERT`ed into
`raw_documents` on the single Fly volume. Unbounded memory read plus unbounded disk
write, on a public endpoint. Fill the volume and SQLite writes start failing — taking
down the ledger, the thing with no backups.

### Fix

```go
func (s *Server) handleEbayDeletion(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // enforced by the reader
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !verifyEbaySignature(r.Header.Get("X-EBAY-SIGNATURE"), body) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// ... archive as before
}
```

Delete `ioReadAll` entirely — `io.ReadAll` already exists and is correct. Implement
`verifyEbaySignature` against eBay's `getPublicKey` API with a cached key. **Until it
is implemented, reject every POST** rather than storing it: an endpoint that accepts
anything is worse than one that accepts nothing.

---

## 5. 🟠 The landed-cost engine has three arithmetic errors

`internal/landedcost/landedcost.go`.

### 5.1 The FX card spread is added to the customs value

```go
cif := itemDst.Add(spread).Add(shipping).Add(insurance)
```

Customs value is the price actually paid for the goods, plus freight and insurance.
**What your card charged you in FX margin is not part of it.** Including `spread` in
CIF inflates the base for both duty and VAT, overstating the tax by roughly
`1.5% × (duty% + VAT%)` — about €90 on the canonical €19,847 example.

The spread is a real cost to the buyer and belongs in the total. It does not belong in
CIF:

```go
cif := twoDP(itemDst.Add(shipping).Add(insurance))
// ... duty and VAT computed from cif ...
// spread is a buyer cost, added outside the customs base
res.Total = twoDP(cif.Add(duty).Add(vat).Add(spread))
```

Relabel the line: `"FX card spread (1.5%) — your cost, not part of customs value"`.

### 5.2 US duty is computed on CIF; it should be on FOB

The seeded rule is `('JP','US','9102.21','5','0','none',...)`. The engine applies
`duty = cif × dutyRate` for every corridor. The US assesses duty on transaction value,
**excluding** international freight and insurance. JP→US duty is therefore overstated
by `5% × (shipping + insurance)`.

The schema has `vat_basis` but no equivalent for duty. Add `duty_basis` (see §1) and
branch:

```go
dutyBase := cif
if rule.DutyBasis == "fob" {
	dutyBase = itemDst
}
duty := twoDP(dutyBase.Mul(rule.DutyRate).Div(money.MustDecimal("100")))
```

`duty_basis = 'fob'` for US destinations, `'cif'` for EU/UK.

### 5.3 The source currency is validated and then thrown away

```go
srcCcy, err := CurrencyFor(rule.FromCountry)
if err != nil {
	return res, err
}
_ = srcCcy
```

`in.Currency` is never checked against the corridor. So
`GET /api/landedcost?from=JP&to=PT&currency=USD&item_price=3200000` returns a
Japan→Portugal breakdown built from a USD price — a ~157× error, silently, with a
ruleset version attached implying it re-derives.

```go
if in.Currency == "" {
	in.Currency = srcCcy
}
if in.Currency != srcCcy {
	return res, fmt.Errorf("corridor %s-%s prices in %s, got %s",
		rule.FromCountry, rule.ToCountry, srcCcy, in.Currency)
}
```

Add a case for it in `landedcost_test.go`.

---

## 6. 🟠 FX is a single frozen row and nothing warns about it

`0005_seed_corridors.sql`:

```sql
INSERT OR IGNORE INTO fx_rates (date, base, rates)
VALUES ('2026-09-01', 'EUR', '{"USD":1.09,"GBP":0.85,"JPY":157.0,"CHF":0.94,"HKD":8.51,"EUR":1.0}');
```

Comment: *"replace with the live ECB fetcher in a later commit."* There is no such
commit — `cmd/` contains no FX binary. `LoadFX(db, "latest")` returns that one row
forever.

There is a 180-day staleness wall for **tax rules** and **none for FX**, even though FX
moves daily and tariff rates move yearly. The staleness discipline is applied to the
stable input and withheld from the volatile one. A 10% JPY move shifts the headline
number by roughly €2,000 with no warning anywhere.

### Fix

**1. Add the fetcher.** `cmd/fxfetch/main.go`, against the ECB daily reference feed:

```go
// ECB publishes daily reference rates, EUR base, ~16:00 CET, weekdays only.
const ecbDaily = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"
```

Parse `Cube[time]` and each `Cube[currency,rate]`, insert `EUR: 1.0` explicitly,
`INSERT OR IGNORE` keyed on date. Run it in the nightly block in `prod-start.sh`
**before** the engine, failing non-fatally like everything else there.

**2. Add an FX staleness wall** in `Compute`, mirroring the rule wall:

```go
const MaxFXAgeDays = 3

if d, err := time.Parse("2006-01-02", fx.Date); err == nil {
	if age := int(time.Since(d).Hours() / 24); age > MaxFXAgeDays {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("FX reference rate is %d days old (%s) — we are re-fetching. "+
				"The converted figures below may have moved.", age, fx.Date))
	}
}
```

**3. Pin FX on every stored evaluation** so G8 holds. `Result` already carries
`FXDate`; confirm `evaluate.SaveEvaluation` persists it and that `LoadEvaluation`
re-renders from the stored value rather than recomputing at `"latest"`.

---

## 7. 🟠 Landed-cost results are not reproducible — G8 does not hold here

`LoadRule` selects `ORDER BY id DESC LIMIT 1`, ignoring `ruleset_version` entirely.
`handleCorridor` calls `LoadFX(s.DB, r.URL.Query().Get("fx_date"))`, which falls back
to `"latest"` when empty. Nothing about a corridor page result is stored.

Yet the page prints:

> "FX date pinned to {{.FXDate}} · ruleset {{.RulesetVersion}} — this breakdown
> re-derives exactly from the cited rules."

There is no way to re-derive it. The next rule row supersedes the last by rowid, and no
result was ever persisted. `ruleset_version` is in the UNIQUE constraint but is never
read by any query. **Versioning is decorative.**

This matters more than it looks. A user who shares a breakdown and returns after you
correct a rate sees different numbers at the same URL, with no record that anything
changed — precisely the failure the architecture exists to prevent.

### Fix

Give landed-cost what verdicts already get in `internal/ledger`:

```sql
-- 0012_landedcost_results.sql
CREATE TABLE landedcost_results (
  inputs_hash     TEXT PRIMARY KEY,   -- sha256 of canonical input JSON
  corridor        TEXT NOT NULL,
  fx_date         TEXT NOT NULL,
  ruleset_version TEXT NOT NULL,
  rule_id         INTEGER NOT NULL,
  content         TEXT NOT NULL,      -- the full Result JSON
  computed_at     INTEGER NOT NULL
);
```

Compute once, store content-addressed, serve from storage on subsequent hits, and make
the shareable URL `/tools/landed-cost/{corridor}/{inputs_hash}`. Then "this breakdown
re-derives exactly" becomes true rather than aspirational.

Add version-aware resolution to `LoadRule` so a stored result can name the exact row it
used.

---

## 8. 🟠 De minimis does not exist anywhere in the system

No `de_minimis` column in `0004_tax_rules.sql`. No threshold logic in `Compute`. No
test in `landedcost_test.go`. But the GTM corridor copy and the UX spec both promise
the state *"No import duty — value is below the €150 threshold."*

Consequences today:

- **EU:** the €150 duty relief still applies. A €120 vintage piece into Portugal is
  charged 4.5% duty by the engine. Wrong.
- **UK:** below £135, VAT is collected by the seller at the point of sale, not at the
  border. The engine tells a UK buyer they owe import VAT on arrival. Wrong, and wrong
  in the direction that makes them over-budget.

### Fix

```sql
ALTER TABLE tax_rules ADD COLUMN duty_de_minimis  TEXT NOT NULL DEFAULT '0';
ALTER TABLE tax_rules ADD COLUMN vat_de_minimis   TEXT NOT NULL DEFAULT '0';
ALTER TABLE tax_rules ADD COLUMN de_minimis_basis TEXT NOT NULL DEFAULT 'intrinsic';
```

`de_minimis_basis` matters: the EU €150 relief is assessed on the **intrinsic value**
of the goods, excluding shipping and insurance — not on CIF. Getting that wrong
reintroduces the same class of error as §5.1.

In `Compute`, before the duty line:

```go
if rule.DutyDeMinimis.IsPositive() && itemDst.LessThan(rule.DutyDeMinimis) {
	duty = money.Zero
	res.Lines = append(res.Lines, Line{
		Label:  "Import duty",
		Amount: money.Zero,
		Basis: fmt.Sprintf("no duty — goods value %s is below the %s %s relief threshold",
			itemDst.StringFixed(2), rule.DutyDeMinimis.StringFixed(2), destCcy),
	})
}
```

For UK below £135, emit a distinct result rather than a zeroed line — the tax does not
disappear, it moves:

> **The seller charges the VAT, not customs.** Below £135 the UK collects VAT at the
> point of sale, so the price you pay the seller should already include 20%. Nothing is
> due when the parcel arrives. If the seller is not UK-VAT-registered this changes —
> ask before you pay.

Set thresholds only on corridors you have verified; leave `0` (feature off) everywhere
else. A wrong threshold is worse than no threshold.

---

## 9. 🟠 Two assumptions the UX spec calls editable are hard-coded

**The card spread:**

```go
const CardSpreadPct = "1.5"
```

A compile-time constant. `Input` has no field for it. The line's own basis text reads
*"edit assumptions if your card differs"* — and there is no way to. The corridor form
in `tools_pages.html` exposes only `price` and `shipping`.

**Shipping:**

```go
shipping = money.MustDecimal("180") // default assumption — surfaced, editable
basis = "default assumption — EDIT ME"
```

180 units of the **destination** currency, for every corridor. €180, £180, $180 — a
single magic number standing in for Japan→Portugal DHL, Switzerland→Germany courier and
US→UK freight alike. And `"EDIT ME"` is developer scaffolding that renders to users in
the basis column.

### Fix

Add both to `Input`, keep the constants as documented fallbacks, and move the shipping
default onto the rule row so it can differ per corridor:

```go
type Input struct {
	// ...
	CardSpreadPct money.Decimal // 0 → DefaultCardSpreadPct
}
```

```sql
ALTER TABLE tax_rules ADD COLUMN typical_shipping TEXT NOT NULL DEFAULT '0';
```

Expose both in the corridor form and in `handleLandedCostAPI`, which already reads
`shipping` and `insurance_pct` from the query string — add `card_spread_pct`.

Replace the `EDIT ME` basis with user-facing copy:

> "Estimated courier + handling. We don't have a quote for this route yet — replace it
> with the seller's number."

**Also missing:** a courier brokerage/disbursement line. DHL, FedEx and UPS charge a fee
for advancing the VAT — typically a flat €10–20 or ~2% of the advanced amount. The GTM
corridor copy tells readers this fee exists and is charged separately. The engine emits
no such line, so the published total is short by that amount. Add it as a rule column
and a line item, or remove the promise from the copy. Do not leave them disagreeing.

---

## 10. 🟠 Alert emails cannot be unsubscribed from

`internal/alerts/alerts.go`:

```go
"You get this because you watch %s. One-click removal: %s (reply STOP and we'll " +
	"remove it by hand — we're small).\n",
newSince, w.Ref, baseURL, w.Ref, w.Ref, baseURL
```

The "one-click removal" link is `baseURL` — the homepage. There is no token, no
unsubscribe route, and no `List-Unsubscribe` header in either
`internal/auth/resend.go` or `internal/auth/smtp.go`.

Since February 2024, Gmail and Yahoo require one-click `List-Unsubscribe` for bulk
senders. Without it, alerts land in spam and the retention loop — the entire Phase 5
mechanism — quietly does nothing. It is also a GDPR problem: "reply STOP and we'll do it
by hand" is not a compliant opt-out mechanism.

### Fix

```sql
-- 0013_unsubscribe.sql
ALTER TABLE users ADD COLUMN unsub_token TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN alerts_off  INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_users_unsub ON users(unsub_token) WHERE unsub_token <> '';
```

Mint the token at user creation in `auth.ConfirmLogin`. Add routes:

```go
mux.HandleFunc("GET /unsubscribe", s.handleUnsubscribe)   // confirmation page
mux.HandleFunc("POST /unsubscribe", s.handleUnsubscribe)  // one-click POST target
```

Extend the `Mailer` interface so headers can be set per message, and send:

```
List-Unsubscribe: <https://watchfairvalue.com/unsubscribe?t=TOKEN>, <mailto:unsubscribe@watchfairvalue.com>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
```

Have `alerts.Run` skip users with `alerts_off = 1`, and change the footer copy:

> You get this because you watch {{ref}}. [Stop these emails]({{link}}) — one click, no
> confirmation needed.

**Also fix the From address.** `resend.go`:

```go
if from == "" {
	from = "WatchLedger <onboarding@resend.dev>"
}
```

If `RESEND_FROM` is unset in production, every email ships from `resend.dev`. Fail
loudly instead:

```go
if m.From == "" {
	return fmt.Errorf("resend: RESEND_FROM not configured — refusing to send")
}
```

And set SPF, DKIM and DMARC on `watchfairvalue.com` before the first alert run:

```
watchfairvalue.com.         TXT  "v=spf1 include:_spf.resend.com ~all"
resend._domainkey...        TXT  (value from the Resend dashboard)
_dmarc.watchfairvalue.com.  TXT  "v=DMARC1; p=none; rua=mailto:dmarc@watchfairvalue.com; pct=100"
```

Start at `p=none`, read the reports for two weeks, then move to `p=quarantine`.

---

## 11. 🟡 Auth and HTTP hardening

Individually small; collectively the difference between a hobby deploy and something
you can point press at.

**Session cookie has no `Secure` flag.** `handleLoginConfirm`:

```go
http.SetCookie(w, &http.Cookie{
	Name: "wl_session", Value: session, Path: "/", HttpOnly: true,
	SameSite: http.SameSiteLaxMode, MaxAge: int(auth.SessionTTL.Seconds()),
})
```

Add `Secure: true`. `fly.toml` already sets `force_https = true`, so there is no cost.

**Open redirect.** Same handler:

```go
if next == "" || !strings.HasPrefix(next, "/") {
	next = "/watchlist"
}
```

`//evil.com` starts with `/`, and browsers treat it as protocol-relative. A crafted
`?next=//evil.com` bounces a freshly authenticated user off-site.

```go
if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
	next = "/watchlist"
}
```

**A hand-rolled, broken escaper:**

```go
func urlQueryEscape(s string) string { return strings_ReplaceAll(s, "&", "%26") }
```

Replace with `url.QueryEscape`. Delete the `strings_ReplaceAll`, `stringsToLower`,
`stringsEqualFold` and `osGetenv` wrappers too — they are one-line aliases of stdlib
functions that are already imported.

**Magic-link consumption is not atomic.** `auth.ConfirmLogin` does
`SELECT ... WHERE used_at IS NULL` and then a separate `UPDATE`. Two concurrent requests
with the same token both succeed. Collapse it:

```go
err = db.QueryRow(`
	UPDATE login_tokens SET used_at = strftime('%s','now')
	WHERE token = ? AND used_at IS NULL
	RETURNING email, expires_at`, tok).Scan(&email, &expires)
```

**Unchecked `rand.Read`.** `newToken()` ignores the error return on the function that
generates every session and login token. It cannot realistically fail on modern Go, but
this is the one place where checking is free and the downside is total.

**No security headers.** `withLogging` is the only middleware:

```go
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// htmx is self-hosted (D8), so no external script origins are needed
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}
```

The templates use inline `<style>` blocks, hence `'unsafe-inline'` for styles only.
`script-src 'self'` is achievable today precisely because htmx is vendored — decision D8
pays off here.

**`/healthz` does not check the database.** It reports ok from uptime alone, so the Fly
health check passes while every page returns 500:

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]any{"status": "db_unavailable"})
		return
	}
	writeJSON(w, map[string]any{"status": "ok", "uptime_s": int(time.Since(s.Started).Seconds())})
}
```

**`POST /api/landedcost` silently behaves as GET.** Both methods route to
`handleLandedCostAPI`, which reads only `r.URL.Query()`. A POST with a body is ignored
and computed from an empty query string. Either parse the body or drop the POST route.

---

## 12. 🟡 Repository hygiene

**Two compiled binaries are committed at the repo root:** `probe` (8.8 MB) and
`syncprod` (9.9 MB). `.gitignore` covers `bin/`, and the `Makefile` builds into `bin/`
— but these two were evidently built with `go build -o probe ./cmd/probe` at root.

18 MB in git history, permanently, plus the supply-chain smell of checked-in
executables. Remove them from the index, add to `.gitignore`, and — since they are
recent — rewrite history now, while it is still cheap:

```
/probe
/syncprod
```

**Verify `.gitignore` line 3.** It appears to read `*.qlite` rather than `*.sqlite`. If
so, a dev database at the repo root is not ignored, and one `git add -A` commits the
entire ledger.

**`internal/httpx/server.go` is ~39 KB in one file** — routing, auth middleware,
templates, corridor pages, the evaluator, the curator admin, the reports admin, sitemap,
robots and the eBay compliance endpoints. Not urgent, except that the fixes above touch
six different concerns inside it. Split along the seams the comments already mark:
`server.go` (wiring + middleware), `auth_routes.go`, `corridor_routes.go`,
`evaluate_routes.go`, `admin_routes.go`, `seo_routes.go`.

**Documentation drift.** The `Server` doc comment states:

> "Routes serve precomputed data only — no live computation on the request path
> (G1: compute lives in cmd/engine)."

`handleReference` calls `engine.ComputeVerdict` on the request path. `handleCorridor`
calls `landedcost.Compute` on the request path. `handleCuratorLedger` calls
`recomputeEngine` — a full ledger recompute — synchronously inside an HTTP handler.

The computation is pure and network-free, so G1's substance holds. But the comment is
false, and a false invariant comment is worse than no comment. Either correct it, or
move the work behind the content-addressed cache from §7 — which would make the comment
true and fix reproducibility in the same change.

**The homepage says the product is under construction.** `templates/index.html`:

> `under construction — Phase 0: the ledger is being built`

The site is live, has a sitemap and is verified in Search Console. Google is currently
indexing a page that describes itself as unfinished. Replace it with the GTM playbook
hero copy before the first launch post.

---

## 13. Revised critical path

The previous plan's critical path was "1, 2, 4, 8" over three weeks, aimed at building
things that already exist. The real path is shorter and sharper, because the work is
**subtraction and verification**, not construction.

### Stage 1 — Stop publishing things you cannot defend

Nothing ships until all four are done. None of them require new features.

1. Gate `LoadRule` and `ListCorridors` on `verified_by NOT IN ('seed','unverified')`
   and non-empty `source_quote` (§1)
2. Verify JP→PT and JP→DE by hand, with verbatim quotes; leave the other eight gated
   off (§1)
3. Litestream replication configured, and **a restore actually tested** (§2)
4. Rate limit `POST /login`; harden or close `POST /ebay/notifications` (§3, §4)

At the end of Stage 1 the site publishes two corridors, both defensible, and cannot be
trivially knocked over or silently lost.

### Stage 2 — Make those two corridors correct

5. Remove the FX spread from CIF; add `duty_basis`; validate source currency (§5)
6. ECB fetcher plus an FX staleness warning (§6)
7. De minimis for the two live corridors — or explicitly document its absence and
   remove the promise from the corridor copy (§8)
8. Card spread and shipping become inputs; add the courier handling line, or cut the
   claim (§9)

### Stage 3 — Make it shareable and safe to promote

9. Content-addressed landed-cost results; permalinks that genuinely re-derive (§7)
10. One-click unsubscribe, `List-Unsubscribe` headers, SPF/DKIM/DMARC, a non-default
    From address (§10)
11. `Secure` cookie, open-redirect fix, security headers, a real `/healthz` (§11)
12. Remove the committed binaries; replace the "under construction" homepage (§12)

Only after Stage 3 does the GTM playbook get executed. Posting a Show HN for a site that
publishes unverified tariff rates from a single unbacked volume converts the one asset
the product has — being the honest one — into the story of why it wasn't.

---

## 14. The two things worth remembering

**One.** The staleness wall, the `source_url` column, the ruleset hash and the
`verified_at` display were all built to make dishonesty structurally difficult. Seeding
ten unverified corridors with `verified_by='seed'` and `verified_at=now()` turned every
one of those mechanisms into a machine for making unverified data look verified. An
integrity apparatus is only as good as the weakest row it renders.

**Two.** `corridor_missing.html` already contains the right answer:

> "We only publish corridors whose tax rules are verified against an official source."

The work in Stage 1 is not adding a feature. It is making that sentence true.

---

*Rules version referenced throughout: `v2026.09.1`. Grounded in
`SalzDevs/watchfetcher@main`. Every finding cites a file; nothing here is inferred from
the planning documents.*
