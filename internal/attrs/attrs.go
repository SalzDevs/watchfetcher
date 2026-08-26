// Package attrs extracts watch attributes (dial, material, reference) from
// free text. Ported from the Python pipeline where they were battle-tested
// against real marketplace markup.
package attrs

import (
	"regexp"
	"strings"
)

// --- dial ------------------------------------------------------------------

var dialNicknames = map[string]string{
	"panda": "panda", "pepsi": "pepsi", "batman": "batman", "batgirl": "batgirl",
	"tiffany": "tiffany", "tropical": "tropical",
}

var dialColors = []string{
	"ice blue", "black", "blue", "green", "white", "silver", "grey", "gray",
	"salmon", "champagne", "meteorite", "brown", "gold",
}

var colorToDial = map[string]string{
	"black": "black", "blue": "blue", "green": "green", "white": "white",
	"silver": "silver", "grey": "grey", "gray": "grey", "salmon": "salmon",
	"champagne": "champagne", "meteorite": "meteorite", "brown": "brown",
	"ice blue": "blue", "gold": "gold",
}

var (
	reColorBeforeDial  = regexp.MustCompile(`\b(` + strings.Join(dialColors, "|") + `)\b[^.;]{0,20}?\bdial\b`)
	reDialBeforeColor  = regexp.MustCompile(`\bdial\b[^.;]{0,20}?\b(` + strings.Join(dialColors, "|") + `)\b`)
	reColorBeforeBezel = regexp.MustCompile(`\b(` + strings.Join(dialColors, "|") + `)\b[^.;]{0,20}?\bbezel\b`)
	reBezelBeforeColor = regexp.MustCompile(`\bbezel\b[^.;]{0,20}?\b(` + strings.Join(dialColors, "|") + `)\b`)
)

// DetectDial classifies the dial/bezel variant from listing text.
// Nicknames ("Pepsi", "Batman") are unambiguous standalone; colors only
// count when adjacent to "dial" or "bezel" so case-material words
// ("yellow gold") never misread as dial colors.
func DetectDial(text string) string {
	t := strings.ToLower(text)
	if t == "" {
		return ""
	}
	for kw, canon := range dialNicknames {
		if wordBoundaryMatch(kw, t) {
			return canon
		}
	}
	for _, re := range []*regexp.Regexp{reColorBeforeDial, reDialBeforeColor, reColorBeforeBezel, reBezelBeforeColor} {
		if m := re.FindStringSubmatch(t); m != nil {
			return colorToDial[m[1]]
		}
	}
	return ""
}

// --- material ---------------------------------------------------------------

var (
	rePlatinum   = regexp.MustCompile(`platin(?:um|e)|\b950\s?pt\b`)
	reTwoTone    = regexp.MustCompile(`two[\s-]?tone|rolesor|bicolou?r|steel\s*(?:and|&|/|\s)\s*(?:18k?\s*)?gold|(?:18k?\s*)?gold\s*(?:and|&|/|\s)\s*steel|acier\s+et\s+or`)
	reGold       = regexp.MustCompile(`\b(?:18|14)[- ]?k(?:arat)?\b|\byg\b|\brg\b|\bwg\b|pink\s+gold|rose\s+gold|yellow\s+gold|white\s+gold|\bgold\b|gelbgold|weissgold|rotgold`)
	reSteel      = regexp.MustCompile(`stainless|\bsteel\b|acier|acciaio|edelstahl`)
	reRefSuffix  = regexp.MustCompile(`\b(\d{4})(?:[./]\d{1,4})*(?:-\d{2,3})*([AJRGPO])(?:\.OO|[^A-Z0-9]|$)`)
	reRefSuffix2 = regexp.MustCompile(`\b\d{5,6}(OR|ST)\.(OO\.[0-9A-Z]{2,8})?`)
)

// DetectMaterial classifies case material. Priority: platinum > two-tone >
// gold > steel > reference-suffix codes (Patek/AP conventions).
func DetectMaterial(text string) string {
	t := strings.ToLower(text)
	if t == "" {
		return ""
	}
	switch {
	case rePlatinum.MatchString(t):
		return "platinum"
	case reTwoTone.MatchString(t):
		return "two_tone"
	case reGold.MatchString(t):
		return "gold"
	case reSteel.MatchString(t):
		return "steel"
	}
	if m := reRefSuffix.FindStringSubmatch(strings.ToUpper(t)); m != nil {
		switch m[2] {
		case "A":
			return "steel"
		case "J", "R", "G":
			return "gold"
		case "P":
			return "platinum"
		}
	}
	if m := reRefSuffix2.FindStringSubmatch(strings.ToUpper(t)); m != nil {
		if m[1] == "OR" {
			return "gold"
		}
		return "steel"
	}
	return ""
}

// --- scope -------------------------------------------------------------------

var (
	reNaked          = regexp.MustCompile(`(?:no|without|sans|ohne|sem)[\s:]*(?:original[\s:]*)?box[\s:,]*(?:and)?[\s:]*(?:no|without|sans|ohne|sem)[\s:]*(?:original[\s:]*)?(?:papers|papiere|papiers|documentos)|\bwatch only\b|\bhead only\b|\bnaked\b`)
	reFullSet        = regexp.MustCompile(`original box\s*,?\s*original papers|\bbox\s*(?:&|and)\s*papers\b|\bfull set\b|\bcomplete set\b|\bfull kit\b|\bb&p\b`)
	reBoxOnly        = regexp.MustCompile(`original box\s*,?\s*no original papers|\bbox only\b|with box[, ]*without papers`)
	rePapersOnly     = regexp.MustCompile(`no original box\s*,?\s*original papers|\bpapers only\b|warranty card only|certificate only`)
	reHasBox         = regexp.MustCompile(`\bbox\b|estojo|etui`)
	reHasPapers      = regexp.MustCompile(`\bpapers\b|papiere|papiers|documentos|card|certificate`)
	reNegPreceding   = regexp.MustCompile(`(?:no|without|sans|ohne|sem)\s*$`)
)

// notNegated reports whether the match at loc[0] is not preceded by a
// negation word ("no", "without") within the preceding 10 characters.
func notNegated(text string, loc []int) bool {
	start := loc[0]
	from := start - 10
	if from < 0 {
		from = 0
	}
	return !reNegPreceding.MatchString(text[from:start])
}

func findAllNotNegated(re *regexp.Regexp, text string) bool {
	for _, loc := range re.FindAllStringIndex(text, -1) {
		if notNegated(text, loc) {
			return true
		}
	}
	return false
}

// DetectScope classifies box & papers completeness. Negations are checked
// before positives so "no original papers" never reads as a full set.
func DetectScope(text string) string {
	low := strings.ToLower(text)
	switch {
	case reNaked.MatchString(low):
		return "naked"
	case findAllNotNegated(reFullSet, low):
		return "full_set"
	case findAllNotNegated(reBoxOnly, low):
		return "box_only"
	case findAllNotNegated(rePapersOnly, low):
		return "papers_only"
	}
	hasBox := reHasBox.MatchString(low)
	hasPapers := reHasPapers.MatchString(low)
	switch {
	case hasBox && hasPapers:
		return "full_set"
	case hasBox:
		return "box_only"
	case hasPapers:
		return "papers_only"
	}
	return ""
}

// --- reference ----------------------------------------------------------------

var refPatterns = []struct {
	re    *regexp.Regexp
	upper bool
}{
	{regexp.MustCompile(`(?i)^(?:3\d{13}|\d{3}\.\d{2}\.\d{2}\.\d{2}\.\d{2}\.\d{3}|\d{3}\.\d{2}\.\d{2}\.\d{2}\.\d{3})$`), false},  // Omega
	{regexp.MustCompile(`(?i)^(?:1[12]\d{4}[A-Z]{0,4}|2[12]\d{4}[A-Z]{0,4}|1[4-7]\d{3}[A-Z]{0,2})$`), true},                    // Rolex
	{regexp.MustCompile(`(?i)^(?:M?79\d{3}[A-Z]{0,2}|M?25\d{3}[A-Z]{0,2})$`), true},                                            // Tudor
	{regexp.MustCompile(`(?i)^(?:CR)?W[A-Z0-9]{7,8}$`), true},                                                                  // Cartier
	{regexp.MustCompile(`(?i)^(?:1[56]\d{3}[A-Z]{2}|[35]\d{3}[A-Z0-9/]{0,3}|IW\d{6}|SBG[A-Z0-9]{3,5})$`), true},                // AP/Patek/IWC
}

var refSplit = regexp.MustCompile(`[/_?#&=+\s\-]+`)
var trailingFile = regexp.MustCompile(`\.(?:html?|php|json)$`)

// ExtractRef finds a reference number in a URL slug or free text.
func ExtractRef(text string) string {
	clean := regexp.MustCompile(`\?.*$`).ReplaceAllString(text, "")
	clean = regexp.MustCompile(`--id\d+`).ReplaceAllString(clean, "")
	clean = trailingFile.ReplaceAllString(clean, "")
	for _, tok := range refSplit.Split(clean, -1) {
		for _, p := range refPatterns {
			if p.re.MatchString(tok) {
				if p.upper {
					return strings.ToUpper(tok)
				}
				return tok
			}
		}
	}
	return ""
}

// --- helpers -------------------------------------------------------------------

func wordBoundaryMatch(kw, t string) bool {
	re := regexp.MustCompile(`\b` + kw + `\b`)
	return re.MatchString(t)
}
