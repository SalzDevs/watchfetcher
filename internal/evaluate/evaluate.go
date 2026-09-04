// Package evaluate: the core job (PLAN.md §9 Phase 4).
// Pure composition over read models: resolve → realised verdict → asks →
// landed cost → position. No verdict language — position vs the band only
// (editorial rules: no "overpriced", no "fair value", no "bargain").
package evaluate

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"watchledger/internal/catalogue"
	"watchledger/internal/engine"
	"watchledger/internal/landedcost"
	"watchledger/internal/money"
)

// Input — what the user gives us (manual form or parsed listing).
type Input struct {
	RefInput     string // raw reference text, resolved via the cascade
	Price        money.Decimal
	Currency     string
	ToCountry    string // landed-cost destination (ISO-2)
	MarginScheme bool
	URL          string // optional, provenance note only
	Now          time.Time
}

// Position — the listing vs the realised band. Positional language only.
type Position struct {
	Sentence  string `json:"sentence"`
	PctVsBand string `json:"pct_vs_band,omitempty"`
}

// Evidence is one frozen comparable (full date precision — re-derivable).
type Evidence struct {
	Source   string `json:"source"`
	Date     string `json:"date"` // YYYY-MM-DD
	Title    string `json:"title"`
	URL      string `json:"url"`
	PriceUSD string `json:"price_usd"`
}

// Result — the full bundle. Stored as JSON on the permalink.
type Result struct {
	ListingHash string `json:"listing_hash"`
	InputsHash  string `json:"inputs_hash"`

	// resolved identity
	Resolved    bool    `json:"resolved"`
	Ref         string  `json:"ref"`
	Brand       string  `json:"brand"`
	Family      string  `json:"family"`
	Dial        string  `json:"dial"`
	Material    string  `json:"material"`
	Confidence  float64 `json:"confidence"`
	NeedsReview bool    `json:"needs_review,omitempty"`

	// realised evidence (exact tier, snapshot)
	RealisedCount    int           `json:"realised_count"`
	GatesStatus      string        `json:"gates_status"` // "" = no evidence
	FailingGates     []string      `json:"failing_gates,omitempty"`
	P10, Median, P90 money.Decimal `json:"-"`
	P10String        string        `json:"p10,omitempty"`
	MedianString     string        `json:"median,omitempty"`
	P90String        string        `json:"p90,omitempty"`
	Evidence         []Evidence    `json:"evidence,omitempty"`
	Houses           []string      `json:"houses,omitempty"`

	// asks context (Phase 3) — only rendered when realised passes gates
	AskMedian string `json:"ask_median,omitempty"`
	AskCount  int    `json:"ask_count,omitempty"`
	SpreadPct string `json:"spread_pct,omitempty"`

	// the listing's position vs the band
	Position Position `json:"position"`

	// landed cost (Phase 1) — may be nil when the corridor is uncovered
	Landed    *landedcost.Result `json:"landed,omitempty"`
	LandedErr string             `json:"landed_err,omitempty"`

	// provenance
	ListingURL     string    `json:"listing_url,omitempty"`
	RulesetVersion string    `json:"ruleset_version"`
	RulesetHash    string    `json:"ruleset_hash"`
	FXDate         string    `json:"fx_date"`
	ComputedAt     time.Time `json:"computed_at"`
}

// Evaluate — pure composition over the DB read models. No network (G1).
func Evaluate(db *sql.DB, in Input) (Result, error) {
	res := Result{ComputedAt: in.Now, RulesetVersion: engine.Current.Version, RulesetHash: engine.Current.Hash(), ListingURL: in.URL}

	// 1. resolve the reference (cascade rungs 2–3; below threshold → ask to confirm)
	resolution, err := catalogue.Lookup(db, in.RefInput)
	if err != nil {
		return res, err
	}
	if resolution.NeedsReview || resolution.Ref == "" {
		res.NeedsReview = true
		res.ListingHash = hashInputs(in)
		return res, nil
	}
	res.Resolved = true
	res.Ref, res.Brand, res.Family = resolution.Ref, resolution.Brand, resolution.Family
	res.Dial, res.Material = resolution.Dial, resolution.Material
	res.Confidence = resolution.Confidence

	// 2. realised evidence — exact tier, auction_realised only (PLAN §7.2)
	obs, evidence, err := realisedFor(db, resolution)
	if err != nil {
		return res, err
	}
	res.RealisedCount = len(obs)
	res.Evidence = evidence
	for _, e := range evidence {
		res.Houses = appendUnique(res.Houses, e.Source)
	}

	// listing price in USD (the band is USD)
	priceUSD, convErr := toUSD(db, in)
	if convErr != nil {
		res.LandedErr = convErr.Error()
	}

	// 3. realised verdict + gates
	var v *engine.Verdict
	if len(obs) > 0 {
		v = engine.ComputeVerdict(resolution.Brand, resolution.Family, resolution.Dial, resolution.Material, "", obs, in.Now)
	}
	if v != nil {
		res.GatesStatus = v.GatesStatus
		res.FailingGates = v.FailingGates
		res.P10, res.Median, res.P90 = v.P10, v.Median, v.P90
		res.P10String, res.MedianString, res.P90String = v.P10.StringFixed(0), v.Median.StringFixed(0), v.P90.StringFixed(0)
		res.InputsHash = v.InputsHash

		// 4. asks context — only when realised is gate-clean (Phase 3 rule)
		if v.GatesStatus == "pass" {
			asks, askCount := asksFor(db, resolution, in.Now)
			if askCount > 0 {
				askMedian := medianDec(asks)
				if askMedian.IsPositive() {
					res.AskMedian = askMedian.StringFixed(0)
					res.AskCount = askCount
					res.SpreadPct = askMedian.Sub(v.Median).Div(v.Median).Mul(money.MustDecimal("100")).Round(1).StringFixed(1)
				}
			}
		}

		// 5. position vs the band — positional language ONLY
		res.Position = positionFor(priceUSD, v)
	} else {
		res.Position = Position{Sentence: "No realised band exists for this reference yet — the evidence below is the whole picture."}
	}

	// 6. landed cost to the user's country (Phase 1 engine)
	if in.ToCountry != "" {
		rule, err := landedcost.LoadRule(db, countryOf(in.Currency, resolution), in.ToCountry)
		if err != nil {
			res.LandedErr = err.Error()
		} else {
			fx, err := landedcost.LoadFX(db, "latest")
			if err != nil {
				res.LandedErr = err.Error()
			} else {
				landed, err := landedcost.Compute(rule, fx, landedcost.Input{
					ItemPrice: in.Price, Currency: in.Currency,
					FromCountry: rule.FromCountry, ToCountry: in.ToCountry,
					MarginScheme: in.MarginScheme, AskPrice: in.Price,
				})
				if err != nil {
					res.LandedErr = err.Error()
				} else {
					res.Landed = &landed
					res.FXDate = landed.FXDate
				}
			}
		}
	}

	res.ListingHash = hashInputs(in)
	return res, nil
}

// realisedFor — auction_realised observations for the resolved ref, with the
// catalogue identity applied (the rows are ref-keyed; the engine needs the tuple).
func realisedFor(db *sql.DB, res catalogue.Resolution) ([]engine.Observation, []Evidence, error) {
	rows, err := db.Query(`
		SELECT source_id, title, url, price_usd, observed_at
		FROM observations
		WHERE kind = 'auction_realised' AND UPPER(ref) = UPPER(?)
		ORDER BY observed_at DESC`, res.Ref)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var obs []engine.Observation
	var evidence []Evidence
	for rows.Next() {
		var o engine.Observation
		var e Evidence
		var price string
		var ts int64
		if err := rows.Scan(&o.Source, &o.Title, &o.URL, &price, &ts); err != nil {
			return nil, nil, err
		}
		d, err := money.FromString(price)
		if err != nil || !d.IsPositive() {
			continue
		}
		o.Brand, o.Model, o.Dial, o.Material = res.Brand, res.Family, res.Dial, res.Material
		o.Ref = res.Ref
		o.ObservedAt = time.Unix(ts, 0)
		o.PriceUSD = d
		obs = append(obs, o)
		e = Evidence{
			Source: o.Source, Date: o.ObservedAt.Format("2006-01-02"),
			Title: o.Title, URL: o.URL, PriceUSD: d.StringFixed(2),
		}
		evidence = append(evidence, e)
	}
	return obs, evidence, rows.Err()
}

// asksFor — current asks (kind='ask', 90 days) for the spread context.
func asksFor(db *sql.DB, res catalogue.Resolution, now time.Time) ([]money.Decimal, int) {
	rows, err := db.Query(`
		SELECT price_usd FROM observations
		WHERE kind = 'ask' AND UPPER(ref) = UPPER(?)
		  AND observed_at >= ? - 90*86400`, res.Ref, now.Unix())
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	var out []money.Decimal
	for rows.Next() {
		var p string
		if rows.Scan(&p) != nil {
			continue
		}
		if d, err := money.FromString(p); err == nil && d.IsPositive() {
			out = append(out, d)
		}
	}
	return out, len(out)
}

func medianDec(ds []money.Decimal) money.Decimal {
	if len(ds) == 0 {
		return money.Zero
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i].LessThan(ds[j]) })
	return ds[len(ds)/2]
}

// positionFor — the listing price vs the realised band (p10–p90).
// Editorial rule: position + numbers, never a quality judgement.
func positionFor(priceUSD money.Decimal, v *engine.Verdict) Position {
	if !priceUSD.IsPositive() {
		return Position{Sentence: "Listing price could not be converted — check the currency."}
	}
	if priceUSD.LessThan(v.P10) {
		pct := v.P10.Sub(priceUSD).Div(v.P10).Mul(money.MustDecimal("100")).Round(1)
		return Position{
			Sentence:  fmt.Sprintf("Asks %s%% below the bottom of the realised band (p10 %s USD).", pct.StringFixed(1), v.P10.StringFixed(0)),
			PctVsBand: "-" + pct.StringFixed(1) + "%",
		}
	}
	if priceUSD.GreaterThan(v.P90) {
		pct := priceUSD.Sub(v.P90).Div(v.P90).Mul(money.MustDecimal("100")).Round(1)
		return Position{
			Sentence:  fmt.Sprintf("Asks %s%% above the top of the realised band (p90 %s USD).", pct.StringFixed(1), v.P90.StringFixed(0)),
			PctVsBand: "+" + pct.StringFixed(1) + "%",
		}
	}
	return Position{
		Sentence:  fmt.Sprintf("Within the realised band (p10 %s – p90 %s USD, median %s).", v.P10.StringFixed(0), v.P90.StringFixed(0), v.Median.StringFixed(0)),
		PctVsBand: "in band",
	}
}

// toUSD — listing price → USD at the pinned FX date.
func toUSD(db *sql.DB, in Input) (money.Decimal, error) {
	fx, err := landedcost.LoadFX(db, "latest")
	if err != nil {
		return money.Zero, err
	}
	rate, err := fx.Rate(in.Currency)
	if err != nil {
		return money.Zero, err
	}
	usd, err := fx.Rate("USD")
	if err != nil {
		return money.Zero, err
	}
	return in.Price.Div(rate).Mul(usd), nil
}

// countryOf — corridor origin guess: the listing's own currency country.
// Always editable in the UI (PLAN.md §8 rule 3).
func countryOf(currency string, res catalogue.Resolution) string {
	switch strings_ToUpper(currency) {
	case "JPY":
		return "JP"
	case "USD":
		return "US"
	case "GBP":
		return "GB"
	case "CHF":
		return "CH"
	case "HKD":
		return "HK"
	default:
		return ""
	}
}

func strings_ToUpper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func hashInputs(in Input) string {
	sum := sha256.Sum256([]byte(strings_Join([]string{
		in.RefInput, in.Price.String(), in.Currency, in.ToCountry,
		fmt.Sprint(in.MarginScheme), in.URL,
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func strings_Join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

// ---- persistence (content-addressed, G4) ----

// SaveEvaluation stores the bundle; same inputs → same listing_hash → no-op.
// Returns the evaluation id for the permalink.
func SaveEvaluation(db *sql.DB, res Result) (int64, error) {
	b, err := json.Marshal(res)
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec(`
		INSERT INTO evaluations (listing_hash, inputs_hash, result, ruleset_version)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(listing_hash) DO NOTHING`,
		res.ListingHash, res.InputsHash, string(b), res.RulesetVersion); err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRow(`SELECT id FROM evaluations WHERE listing_hash = ?`, res.ListingHash).Scan(&id)
	return id, err
}

// LoadEvaluation — the permalink read.
func LoadEvaluation(db *sql.DB, id int64) (*Result, error) {
	var raw string
	err := db.QueryRow(`SELECT result FROM evaluations WHERE id = ?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var res Result
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// RebuildEvidence — reconstruct engine observations from the stored evidence
// snapshot (full dates) — the reproduce path for evaluations (G8).
func (r *Result) RebuildEvidence(res catalogue.Resolution) []engine.Observation {
	out := make([]engine.Observation, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		p, err := money.FromString(e.PriceUSD)
		if err != nil {
			continue
		}
		out = append(out, engine.Observation{
			Brand: r.Brand, Model: r.Family, Dial: r.Dial, Material: r.Material, Ref: r.Ref,
			Source: e.Source, Title: e.Title, URL: e.URL,
			PriceUSD: p, ObservedAt: d,
		})
	}
	return out
}
