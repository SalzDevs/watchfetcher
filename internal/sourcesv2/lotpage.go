// Lot-page enrichment: refs often live in the lot description, not the title.
// The lot page exposes them via <meta name="description">.
package sourcesv2

import (
	"html"
	"regexp"
	"strings"
)

var (
	reMetaDescription = regexp.MustCompile(`<meta name="description" content="([^"]+)"`)
	reRefInText       = regexp.MustCompile(`(?i)\b(?:ref(?:erence)?\.?\s*:?\s*)([0-9A-Z][0-9A-Z/.\-]{3,13})`)
)

// ExtractLotMetaDescription — the lot page's structured description
// ("Model: … Reference: 3131 Date: …"), unescaped. Empty when absent.
func ExtractLotMetaDescription(raw []byte) (string, error) {
	m := reMetaDescription.FindSubmatch(raw)
	if m == nil {
		return "", nil // present-but-empty is not an error; caller logs and moves on
	}
	return html.UnescapeString(strings.TrimSpace(string(m[1]))), nil
}
