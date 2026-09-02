package landedcost

import "fmt"

// CountryName — the corridor set. Adding a corridor = add a rule row + these
// two entries. Boring, explicit, complete.
var CountryName = map[string]string{
	"JP": "Japan", "US": "United States", "GB": "United Kingdom", "UK": "United Kingdom",
	"CH": "Switzerland", "HK": "Hong Kong", "PT": "Portugal", "DE": "Germany",
	"FR": "France", "IT": "Italy", "ES": "Spain", "NL": "Netherlands",
}

// SlugFor builds the canonical corridor slug: 'japan-to-portugal'.
func SlugFor(from, to string) string {
	return fmt.Sprintf("%s-to-%s", slug(CountryName[from]), slug(CountryName[to]))
}

// ParseSlug — 'japan-to-portugal' → ('JP','PT', true).
func ParseSlug(slug string) (string, string, bool) {
	bySlug := map[string]string{}
	for iso, name := range CountryName {
		bySlug[slugify(name)] = iso
	}
	// split on '-to-' (country slugs never contain '-to-')
	for i := 0; i+len("-to-") <= len(slug); i++ {
		if slug[i:i+4] != "-to-" {
			continue
		}
		from, to := slug[:i], slug[i+4:]
		f, okF := bySlug[from]
		t, okT := bySlug[to]
		if okF && okT {
			return f, t, true
		}
	}
	return "", "", false
}

func slug(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ':
			out = append(out, '-')
		}
	}
	return string(out)
}

func slugify(s string) string { return slug(s) }
