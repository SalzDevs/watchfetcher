// Package catalogue: reference resolution via the cascade (PLAN.md §7.1).
// Rung 1 structured field is upstream (adapters); this package implements
// rung 2 (exact normalised ref, 0.98) and rung 3 (alias, 0.90).
// Below 0.85 never auto-accepts — fuzzy goes to review queue in Phase 2.
package catalogue

import (
	"database/sql"
	"fmt"
	"strings"
)

const (
	RungExact     = 2
	RungAlias     = 3
	ConfExact     = 0.98
	ConfAlias     = 0.90
	AutoAcceptMin = 0.85
)

type Resolution struct {
	Ref         string
	Brand       string
	Family      string
	Dial        string
	Material    string
	Confidence  float64
	Rung        int
	NeedsReview bool
}

// Lookup resolves a reference string against the catalogue.
// Never guesses: miss = NeedsReview, no fabricated ref.
func Lookup(db *sql.DB, input string) (Resolution, error) {
	key := normalise(input)
	if key == "" {
		return Resolution{}, nil
	}

	// Rung 2: exact normalised match. Marketplaces truncate '5711/1A-010' to
	// '5711/1A' or '5711' — prefix aggregation with ambiguity rejection.
	res, ok, err := lookupExact(db, key)
	if err != nil {
		return Resolution{}, err
	}
	if ok {
		return res, nil
	}

	// Rung 3: alias.
	res, ok, err = lookupAlias(db, strings.ToLower(key))
	if err != nil {
		return Resolution{}, err
	}
	if ok {
		return res, nil
	}

	return Resolution{NeedsReview: true}, nil
}

func lookupExact(db *sql.DB, key string) (Resolution, bool, error) {
	var r Resolution
	var dial, material string
	err := db.QueryRow(`
		SELECT ref, brand, family, dial, material FROM catalogue_references WHERE UPPER(ref) = ?`,
		key).Scan(&r.Ref, &r.Brand, &r.Family, &dial, &material)
	if err == sql.ErrNoRows {
		// prefix: key is the base of 'base/suffix' refs; ambiguity matches nothing
		rows, err := db.Query(`
			SELECT ref, brand, family, dial, material FROM catalogue_references
			WHERE UPPER(ref) LIKE ? || '/%' OR UPPER(ref) LIKE ? || '-%'`, key, key)
		if err != nil {
			return Resolution{}, false, err
		}
		defer rows.Close()
		var hit *Resolution
		for rows.Next() {
			cand := Resolution{Rung: RungExact, Confidence: ConfExact}
			if err := rows.Scan(&cand.Ref, &cand.Brand, &cand.Family, &dial, &material); err != nil {
				return Resolution{}, false, err
			}
			cand.Dial, cand.Material = dial, material
			if hit == nil {
				c := cand
				hit = &c
				continue
			}
			if *hit != cand {
				return Resolution{}, false, nil // ambiguous base — refuse, never guess
			}
		}
		if hit != nil {
			return *hit, true, rows.Err()
		}
		return Resolution{}, false, rows.Err()
	}
	if err != nil {
		return Resolution{}, false, err
	}
	r.Dial, r.Material = dial, material
	r.Rung, r.Confidence = RungExact, ConfExact
	return r, true, nil
}

func lookupAlias(db *sql.DB, alias string) (Resolution, bool, error) {
	var r Resolution
	var dial, material string
	var refs int
	// one alias must map to exactly one ref — conflicting alias rows need review
	err := db.QueryRow(`SELECT COUNT(DISTINCT ref) FROM catalogue_aliases WHERE LOWER(alias) = ?`, alias).Scan(&refs)
	if err != nil {
		return Resolution{}, false, err
	}
	if refs != 1 {
		return Resolution{}, false, nil
	}
	err = db.QueryRow(`
		SELECT cr.ref, cr.brand, cr.family, cr.dial, cr.material
		FROM catalogue_aliases ca JOIN catalogue_references cr ON cr.ref = ca.ref
		WHERE LOWER(ca.alias) = ?`, alias).Scan(&r.Ref, &r.Brand, &r.Family, &dial, &material)
	if err == sql.ErrNoRows {
		return Resolution{}, false, nil
	}
	if err != nil {
		return Resolution{}, false, err
	}
	r.Dial, r.Material = dial, material
	r.Rung, r.Confidence = RungAlias, ConfAlias
	return r, true, nil
}

// normalise — uppercase, strip spaces; keep '/', '-' and '.' (Patek/Omega forms).
func normalise(s string) string {
	return strings.ToUpper(strings.TrimSpace(strings.Join(strings.Fields(s), "")))
}

var _ = fmt.Sprintf // keep fmt until review-queue helpers land
