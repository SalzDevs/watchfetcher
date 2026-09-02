package catalogue

import "database/sql"

// FamilyRefs — every catalogued reference in one brand+family, sorted by ref.
func FamilyRefs(db *sql.DB, brand, family string) ([]string, error) {
	rows, err := db.Query(`
		SELECT ref FROM catalogue_references
		WHERE brand = ? AND family = ? ORDER BY ref`, brand, family)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) == nil {
			out = append(out, ref)
		}
	}
	return out, rows.Err()
}
