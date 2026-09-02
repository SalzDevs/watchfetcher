package sourcesv2

import (
	"regexp"
	"sort"
)

// Reference-number patterns per maison, ordered by specificity. First match
// set wins; catalogue resolution decides which is real (never guessed here).
var refPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{"omega", regexp.MustCompile(`\b\d{3}\.\d{2}\.\d{2}\.\d{2}\.\d{2}\.\d{3}\b`)},
	{"patek", regexp.MustCompile(`(?i)\b[0-9]{4}/[0-9A-Z]{1,6}(?:-[0-9A-Z]{1,6}){0,2}\b`)},
	{"patek", regexp.MustCompile(`(?i)\b[0-9]{4}[A-Z](?:/[0-9A-Z]+)?-[0-9]{3}\b`)},
	{"rolex", regexp.MustCompile(`(?i)\b1[0-9]{5}[A-Z]{0,4}\b`)},
	{"tudor", regexp.MustCompile(`(?i)\bM?79[0-9]{3}[A-Z]{0,2}\b`)},
	{"iwc", regexp.MustCompile(`(?i)\bIW\d{6}\b`)},
	{"cartier", regexp.MustCompile(`(?i)\b(?:W|Q)[A-Z0-9]{7}\b`)},
	{"panerai", regexp.MustCompile(`(?i)\bPAM\d{5}\b`)},
	{"grandseiko", regexp.MustCompile(`(?i)\bSBG[A-Z0-9]{4,5}\b`)},
}

// ExtractRefCandidates returns unique reference-number candidates from lot
// text, uppercased, most-specific pattern order preserved.
func ExtractRefCandidates(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range refPatterns {
		for _, m := range p.re.FindAllString(text, -1) {
			up := m
			if p.label != "omega" {
				up = upper(m)
			}
			if !seen[up] {
				seen[up] = true
				out = append(out, up)
			}
		}
	}
	return out
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - 32
		}
	}
	return string(out)
}

var _ = sort.Ints
