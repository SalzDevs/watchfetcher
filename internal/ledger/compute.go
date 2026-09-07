// Compute run: ledger → content-addressed verdicts. Shared by cmd/engine
// (nightly) and the curator ledgering (immediate recompute).
package ledger

import (
	"database/sql"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/money"
)

// RunCompute — read realised-tier observations, group by exact cell, compute
// content-addressed verdicts. Pure compute over storage, no network (G1).
func RunCompute(db *sql.DB, now time.Time) (written, limited, obsCount int, err error) {
	rows, err := db.Query(`
		SELECT brand, model, dial, material, scope, ref, kind, source_id,
		       title, url, price_usd, observed_at
		FROM observations
		WHERE price_usd IS NOT NULL AND kind = 'auction_realised'`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()

	cells := map[string][]engine.Observation{}
	for rows.Next() {
		var o engine.Observation
		var priceUSD string
		var observedAt int64
		if err := rows.Scan(&o.Brand, &o.Model, &o.Dial, &o.Material, &o.Scope, &o.Ref,
			&o.Kind, &o.Source, &o.Title, &o.URL, &priceUSD, &observedAt); err != nil {
			return 0, 0, 0, err
		}
		d, perr := money.FromString(priceUSD)
		if perr != nil {
			continue // corrupt price — skip, never fabricate
		}
		o.PriceUSD = d
		o.ObservedAt = time.Unix(observedAt, 0)
		key := engine.CellKey(o.Brand, o.Model, o.Dial, o.Material, o.Scope)
		cells[key] = append(cells[key], o)
		obsCount++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}

	for key, obs := range cells {
		p := splitCellKey(key)
		v := engine.ComputeVerdict(p[0], p[1], p[2], p[3], p[4], obs, now)
		if v == nil {
			continue
		}
		if err := SaveVerdictContent(db, v); err != nil {
			return written, limited, obsCount, err
		}
		written++
		if v.GatesStatus == "limited" {
			limited++
		}
	}
	return written, limited, obsCount, nil
}

func splitCellKey(cell string) []string {
	var out []string
	cur := ""
	for _, r := range cell {
		if r == '|' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
