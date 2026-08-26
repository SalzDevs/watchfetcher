// Package sources: one adapter per marketplace, all producing canonical
// model.Listing structs. Each adapter owns its fetching + parsing strategy.
package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"watchfetcher/internal/httpclient"
	"watchfetcher/internal/model"
)

// Source is one marketplace adapter. Implementations must be safe for
// concurrent use and never invent data: unknown attributes stay empty.
type Source interface {
	ID() string
	Name() string
	Fetch(ctx context.Context, client *httpclient.Client, brand, model string, maxPerModel int) ([]model.Listing, error)
}

// --- shared helpers ---------------------------------------------------------

// WalkJSON visits every JSON object in a decoded JSON-LD tree.
func WalkJSON(node any, visit func(map[string]any)) {
	switch v := node.(type) {
	case map[string]any:
		visit(v)
		for _, val := range v {
			WalkJSON(val, visit)
		}
	case []any:
		for _, val := range v {
			WalkJSON(val, visit)
		}
	}
}

// ExtractJSONLD returns the decoded payloads of all JSON-LD script blocks.
func ExtractJSONLD(htmlText string) []any {
	var out []any
	re := regexp.MustCompile(`(?s)<script[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	for _, m := range re.FindAllStringSubmatch(htmlText, -1) {
		var decoded any
		if err := json.Unmarshal([]byte(m[1]), &decoded); err == nil {
			out = append(out, decoded)
		}
	}
	return out
}

// CollectOffers walks JSON-LD trees and collects every "offers" array entry.
func CollectOffers(trees []any) []map[string]any {
	var offers []map[string]any
	for _, tree := range trees {
		WalkJSON(tree, func(node map[string]any) {
			if raw, ok := node["offers"].([]any); ok {
				for _, o := range raw {
					if om, ok := o.(map[string]any); ok {
						offers = append(offers, om)
					}
				}
			}
		})
	}
	return offers
}

var stripTagsRe = regexp.MustCompile(`<[^>]+>`)
var spaceRe = regexp.MustCompile(`\s+`)

// StripTags removes HTML tags and collapses whitespace.
func StripTags(s string) string {
	return spaceRe.ReplaceAllString(stripTagsRe.ReplaceAllString(s, ""), "")
}

// ParseLocalePrice handles '12,495', '5.200', '12.500,50', '1,234.56'.
func ParseLocalePrice(raw any) float64 {
	s := strings.TrimSpace(fmt.Sprint(raw))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u00a0", "")
	var neg bool
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	if strings.Contains(s, ",") && strings.Contains(s, ".") {
		if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	} else if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts[len(parts)-1]) == 3 {
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.Replace(s, ",", ".", 1)
		}
	} else if strings.Contains(s, ".") {
		parts := strings.Split(s, ".")
		if len(parts) == 2 && len(parts[1]) == 3 {
			s = strings.ReplaceAll(s, ".", "") // EU thousands dot
		}
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return 0
	}
	if neg {
		v = -v
	}
	return v
}

func derefString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// --- query gating ------------------------------------------------------------

var compactRe = regexp.MustCompile(`[^a-z0-9]+`)

// compact normalizes text for token containment checks.
func compact(s string) string {
	return compactRe.ReplaceAllString(strings.ToLower(s), "")
}

// matchesQuery gates listings to the browse selection: brand must agree when
// both sides state it; every model token must appear in the listing's
// title/model (token-based so word order never matters).
func matchesQuery(brand, model string, l *model.Listing) bool {
	if l == nil {
		return false
	}
	if brand != "" && l.Brand != "" &&
		!strings.Contains(strings.ToLower(l.Brand), strings.ToLower(brand)) {
		return false
	}
	if model != "" {
		hay := compact(l.Title + " " + l.Model)
		for _, tok := range strings.Fields(strings.ToLower(model)) {
			if ct := compact(tok); ct != "" && !strings.Contains(hay, ct) {
				return false
			}
		}
	}
	return true
}

// NativeIDFromURL derives a stable per-listing ID from its URL path when the
// source doesn't expose a native ID field.
func NativeIDFromURL(rawURL string) string {
	u := strings.Split(rawURL, "?")[0]
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".html")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return u
}
