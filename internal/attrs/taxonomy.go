package attrs

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The curated taxonomy: 20 maisons and their model families. This is the
// single source of truth for brand/model detection at engine ingest.
var brandModels = map[string][]string{
	"Rolex": {
		"Submariner Date", "Submariner No-Date", "GMT-Master II",
		"Cosmograph Daytona", "Datejust", "Day-Date", "Explorer II",
		"Explorer", "Sea-Dweller", "Yacht-Master", "Sky-Dweller",
		"Air-King", "Oyster Perpetual",
	},
	"Patek Philippe":    {"Nautilus", "Aquanaut", "Calatrava", "Golden Ellipse"},
	"Audemars Piguet":   {"Royal Oak Offshore", "Royal Oak"},
	"Vacheron Constantin": {"Overseas", "Patrimony", "Traditionnelle", "Historiques"},
	"Richard Mille":     {"RM 011", "RM 030", "RM 35", "RM 67"},
	"Omega": {
		"Speedmaster Professional", "Speedmaster", "Seamaster Diver 300M",
		"Seamaster Aqua Terra", "Seamaster Planet Ocean", "Constellation",
	},
	"Cartier":  {"Santos de Cartier", "Tank Must", "Ballon Bleu", "Panthère", "Santos-Dumont"},
	"Breitling": {"Navitimer", "Chronomat", "Superocean Heritage", "Avenger", "Premier"},
	"Jaeger-LeCoultre": {"Reverso", "Master Control", "Polaris"},
	"IWC":              {"Big Pilot", "Portugieser", "Pilot's Watch Chronograph", "Ingenieur"},
	"Breguet":          {"Type XX", "Classique", "Marine", "Tradition"},
	"A. Lange & Söhne": {"Lange 1", "Saxonia", "Odysseus", "1815"},
	"Tudor": {
		"Black Bay 58", "Black Bay GMT", "Black Bay Chrono", "Black Bay",
		"Pelagos", "Royal",
	},
	"Hublot":    {"Classic Fusion", "Big Bang"},
	"Panerai":   {"Luminor Marina", "Luminor", "Radiomir", "Submersible"},
	"TAG Heuer": {"Carrera", "Monaco", "Aquaracer", "Formula 1"},
	"Longines":  {"Master Collection", "Conquest", "HydroConquest", "Spirit"},
	"Grand Seiko": {"Snowflake", "Spring Drive GMT", "Hi-Beat 36000"},
	"Chopard":   {"Happy Sport", "Mille Miglia", "L.U.C"},
	"Bulgari":   {"Octo Finissimo", "Octo Roma", "Serpenti", "Aluminium"},
}

// Brands returns the sorted list of known brands.
func Brands() []string {
	out := make([]string, 0, len(brandModels))
	for b := range brandModels {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

var brandAliases = map[string]string{
	"jlc": "Jaeger-LeCoultre", "ap": "Audemars Piguet",
	"vc": "Vacheron Constantin", "gs": "Grand Seiko",
	"als": "A. Lange & Söhne",
}

var brandsSorted []string

func init() {
	brandsSorted = Brands()
}

// DetectBrand finds a known brand in text (compact substring match).
func DetectBrand(text string) string {
	c := compactText(text)
	for _, b := range brandsSorted {
		if strings.Contains(c, compactText(b)) {
			return b
		}
	}
	// abbreviations via word boundary on raw text
	low := strings.ToLower(text)
	for alias, full := range brandAliases {
		if containsWord(low, alias) {
			return full
		}
	}
	return ""
}

var (
	modelRegexes     []struct {
		name string
		re   *regexp.Regexp
	}
	modelRegexOnce sync.Once
)

func initModelRegexes() {
	for _, models := range brandModels {
		for _, m := range models {
			words := strings.Fields(m)
			if len(words) == 0 {
				continue
			}
			parts := make([]string, 0, len(words))
			for _, w := range words {
				parts = append(parts, regexp.QuoteMeta(w))
			}
			pattern := `\b` + strings.Join(parts, `[\s\-]*`) + `\b`
			modelRegexes = append(modelRegexes, struct {
				name string
				re   *regexp.Regexp
			}{m, regexp.MustCompile(`(?i)` + pattern)})
		}
	}
	// longest pattern wins: "Black Bay 58" beats "Black Bay"
	sort.Slice(modelRegexes, func(i, j int) bool {
		return len(modelRegexes[i].re.String()) > len(modelRegexes[j].re.String())
	})
}

// DetectModel finds the longest matching known model in text using
// word-boundary-aware matching — "Marine" never matches inside "Submariner".
func DetectModel(text string) string {
	modelRegexOnce.Do(initModelRegexes)
	for _, mr := range modelRegexes {
		if mr.re.MatchString(text) {
			return mr.name
		}
	}
	return ""
}

// DetectModelFamily finds family-level model tokens from informal input
// ("gmt" -> "GMT-Master II") when the full model name isn't stated.
func DetectModelFamily(text string) string {
	low := strings.ToLower(text)
	type candidate struct {
		aliasLen int
		family   string
	}
	var bestFamily string
	for _, models := range brandModels {
		for _, m := range models {
			words := strings.Fields(strings.ToLower(m))
			if len(words) == 0 {
				continue
			}
			first := words[0]
			if containsWord(low, first) && len(first) > len(bestFamily) {
				bestFamily = first
			}
		}
	}
	if bestFamily != "" {
		return strings.ToUpper(bestFamily[:1]) + bestFamily[1:]
	}
	// known informal families
	families := []string{"submariner", "daytona", "nautilus", "royal oak", "speedmaster"}
	for _, f := range families {
		if containsWord(low, f) {
			return strings.ToUpper(f[:1]) + f[1:]
		}
	}
	return ""
}

func containsWord(text, word string) bool {
	return regexpWordBoundary(word).MatchString(text)
}

var wordBoundaryCache = map[string]*regexp.Regexp{}

func regexpWordBoundary(word string) *regexp.Regexp {
	if re, ok := wordBoundaryCache[word]; ok {
		return re
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
	wordBoundaryCache[word] = re
	return re
}

func compactText(s string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(s) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		}
	}
	return b.String()
}
