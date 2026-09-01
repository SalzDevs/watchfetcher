package attrs

import (
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:generate go run ../../tools/genref

// Reference is one factory SKU: an indivisible {case, bracelet, dial, bezel,
// material} combination. You don't configure 126610LN with a green dial —
// that watch is a 126610LV. Dial and material are derived from the reference,
// never free-text.
type Reference struct {
	Ref      string
	Brand    string
	Model    string
	Dial     string // "" = reference exists but dial not pinned in taxonomy
	Material string
}

// LookupRef resolves a reference number to its factory configuration.
// Exact match first. If the input is a base prefix of catalogued refs
// (marketplaces truncate "5711/1A-010" to "5711"), all matching entries must
// agree on {brand, model, dial, material} — an ambiguous prefix (5711 = blue
// steel OR brown gold) matches nothing rather than guessing.
func LookupRef(ref string) (Reference, bool) {
	key := strings.ToUpper(strings.TrimSpace(ref))
	if key == "" {
		return Reference{}, false
	}
	if e, ok := referenceIndex[key]; ok {
		return Reference{Ref: key, Brand: e.Brand, Model: e.Model, Dial: e.Dial, Material: e.Material}, true
	}
	// Prefix aggregation: ref is a base of "base/suffix" (5711/1A-010) or
	// "base-dialcode" (5167A-001) style references — marketplaces truncate both.
	var hit *refEntry
	for k, e := range referenceIndex {
		if !strings.HasPrefix(k, key+"/") && !strings.HasPrefix(k, key+"-") {
			continue
		}
		if hit == nil {
			e := e
			hit = &e
			continue
		}
		// Ambiguous: two catalogued refs share this base with different configs.
		if *hit != e {
			return Reference{}, false
		}
	}
	if hit != nil {
		return Reference{Ref: key, Brand: hit.Brand, Model: hit.Model, Dial: hit.Dial, Material: hit.Material}, true
	}
	return Reference{}, false
}

// ReferencesForModel returns the catalogued references for one brand+model
// family, sorted by ref. Empty slice when the family has no catalogued refs
// (vintage / free-dial models).
func ReferencesForModel(brand, model string) []Reference {
	var out []Reference
	for ref, e := range referenceIndex {
		if strings.EqualFold(e.Brand, strings.TrimSpace(brand)) && strings.EqualFold(e.Model, strings.TrimSpace(model)) {
			out = append(out, Reference{Ref: ref, Brand: e.Brand, Model: e.Model, Dial: e.Dial, Material: e.Material})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

// ReferenceCount returns the total number of catalogued references.
func ReferenceCount() int { return len(referenceIndex) }

// The curated taxonomy: 20 maisons and their model families — 2025 catalogue.
// This is the single source of truth for brand/model detection at engine ingest.
// Generated from config/models.json — do not edit by hand (go generate).
var brandModels = map[string][]string{
	"Rolex": {
		"Submariner Date", "Submariner No-Date", "GMT-Master II",
		"Cosmograph Daytona", "Datejust", "Day-Date", "Explorer II",
		"Explorer", "Sea-Dweller", "Yacht-Master", "Yacht-Master II", "Sky-Dweller",
		"Air-King", "Oyster Perpetual", "Milgauss", "Land-Dweller", "Perpetual 1908",
	},
	"Patek Philippe":    {"Nautilus", "Aquanaut", "Calatrava", "Golden Ellipse", "Twenty~4", "Complications", "Grand Complications", "Cubitus"},
	"Audemars Piguet":   {"Royal Oak", "Royal Oak Offshore", "Code 11.59"},
	"Vacheron Constantin": {"Overseas", "Patrimony", "Traditionnelle", "Historiques"},
	"Richard Mille":     {"RM 011", "RM 030", "RM 35", "RM 67"},
	"Omega": {
		"Speedmaster Professional", "Speedmaster", "Seamaster Diver 300M",
		"Seamaster Aqua Terra", "Seamaster Planet Ocean", "Constellation",
		"Railmaster", "Seamaster Ploprof",
	},
	"Cartier":  {"Santos de Cartier", "Tank Must", "Ballon Bleu", "Panthère", "Santos-Dumont"},
	"Breitling": {"Navitimer", "Chronomat", "Superocean Heritage", "Avenger", "Premier"},
	"Jaeger-LeCoultre": {"Reverso", "Master Control", "Polaris"},
	"IWC":              {"Big Pilot", "Portugieser", "Pilot's Watch Chronograph", "Ingenieur"},
	"Breguet":          {"Type XX", "Classique", "Marine", "Tradition"},
	"A. Lange & Söhne": {"Lange 1", "Saxonia", "Odysseus", "1815"},
	"Tudor": {
		"Black Bay 58", "Black Bay GMT", "Black Bay Chrono", "Black Bay",
		"Pelagos", "Royal", "Ranger",
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

// ModelsForBrand returns sorted models for a brand (case-insensitive), or nil if unknown.
func ModelsForBrand(brand string) []string {
	for b, ms := range brandModels {
		if strings.EqualFold(b, strings.TrimSpace(brand)) {
			out := make([]string, len(ms))
			copy(out, ms)
			sort.Strings(out)
			return out
		}
	}
	return nil
}

// AllModels returns all models across brands sorted (for search).
func AllModels() []string {
	var out []string
	for _, ms := range brandModels {
		out = append(out, ms...)
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

// CuratedImagePath is the convention for curated reference art:
// /watches/{brand-slug}/{model-slug}/{REF}.webp. Missing files fall back to
// receipt images (onError in the frontend) — curated is an override, not a
// requirement. Used by API responses and the image harvester.
func CuratedImagePath(brand, model, ref string) string {
	slug := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		var b strings.Builder
		dash := false
		for _, c := range s {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
				if dash {
					b.WriteRune('-')
					dash = false
				}
				b.WriteRune(c)
			} else if b.Len() > 0 {
				dash = true
			}
		}
		return b.String()
	}
	return "/watches/" + slug(brand) + "/" + slug(model) + "/" + strings.ToUpper(strings.TrimSpace(ref)) + ".webp"
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
