// DB loaders for the landed-cost read path. Kept separate from the pure engine.
package landedcost

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"watchledger/internal/money"
)

// LoadRule — the verified corridor rule, by ISO country pair.
func LoadRule(db *sql.DB, from, to string) (Rule, error) {
	var r Rule
	var verifiedAt int64
	err := db.QueryRow(`
		SELECT from_country, to_country, hs_code, duty_rate, vat_rate, vat_basis,
		       insurance_pct, basis, source_url, verified_at, verified_by, ruleset_version
		FROM tax_rules WHERE from_country = ? AND to_country = ?
		ORDER BY id DESC LIMIT 1`, from, to).Scan(
		&r.FromCountry, &r.ToCountry, &r.HsCode, &r.DutyRate, &r.VatRate, &r.VatBasis,
		&r.InsurancePct, &r.Basis, &r.SourceURL, &verifiedAt, &r.VerifiedBy, &r.RulesetVersion)
	if err == sql.ErrNoRows {
		return Rule{}, fmt.Errorf("no verified rule for corridor %s-%s", from, to)
	}
	if err != nil {
		return Rule{}, err
	}
	r.VerifiedAt = time.Unix(verifiedAt, 0)
	return r, nil
}

// ListCorridors — all seeded corridors for the index page.
func ListCorridors(db *sql.DB) ([]Rule, error) {
	rows, err := db.Query(`
		SELECT from_country, to_country, hs_code, duty_rate, vat_rate, basis,
		       source_url, verified_at, ruleset_version
		FROM tax_rules ORDER BY from_country, to_country`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var verifiedAt int64
		if err := rows.Scan(&r.FromCountry, &r.ToCountry, &r.HsCode, &r.DutyRate,
			&r.VatRate, &r.Basis, &r.SourceURL, &verifiedAt, &r.RulesetVersion); err != nil {
			return nil, err
		}
		r.VerifiedAt = time.Unix(verifiedAt, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LoadFX — pinned date, or latest when date is empty/'latest'.
func LoadFX(db *sql.DB, date string) (FX, error) {
	var d, rates string
	var err error
	if date == "" || date == "latest" {
		err = db.QueryRow(`SELECT date, rates FROM fx_rates ORDER BY date DESC LIMIT 1`).Scan(&d, &rates)
	} else {
		err = db.QueryRow(`SELECT date, rates FROM fx_rates WHERE date = ?`, date).Scan(&d, &rates)
	}
	if err == sql.ErrNoRows {
		return FX{}, fmt.Errorf("no FX rates stored for %q", date)
	}
	if err != nil {
		return FX{}, err
	}
	var m map[string]money.Decimal
	if err := json.Unmarshal([]byte(rates), &m); err != nil {
		return FX{}, fmt.Errorf("corrupt fx rates: %w", err)
	}
	return FX{Date: d, Rates: m}, nil
}

var _ = strings.TrimSpace
