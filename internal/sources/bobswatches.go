package sources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/httpclient"
	wfmodel "watchfetcher/internal/model"
)

// BobswatchesSource: brand-pathed catalog pages that embed one JSON-LD
// Product block per listing (name / mpn=reference / offers.price / color).
// Brand required — their catalog is organized by brand path.
type BobswatchesSource struct {
	MaxPages int
}

func (b *BobswatchesSource) ID() string   { return "bobswatches" }
func (b *BobswatchesSource) Name() string { return "Bob's Watches" }

var bobSlugs = map[string]string{
	"Rolex": "rolex", "Omega": "omega", "Tudor": "tudor", "Cartier": "cartier",
	"Patek Philippe": "patek-philippe", "Audemars Piguet": "audemars-piguet",
}

func (b *BobswatchesSource) slugFor(brand string) (string, bool) {
	if s, ok := bobSlugs[brand]; ok {
		return s, true
	}
	return strings.ToLower(strings.ReplaceAll(brand, " ", "-")), false
}

func (b *BobswatchesSource) Fetch(ctx context.Context, client *httpclient.Client, brand, model string, maxPerModel int) ([]wfmodel.Listing, error) {
	if brand == "" {
		return nil, fmt.Errorf("bobswatches: searches require a brand (catalog is brand-pathed)")
	}
	slug, _ := b.slugFor(brand)
	terms := strings.TrimSpace(model)
	var out []wfmodel.Listing
	seen := map[string]bool{}
	maxPages := 3

	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("https://www.bobswatches.com/%s?query=%s&page=%d", slug, url.QueryEscape(terms), page)
		status, body, err := client.Get(ctx, url)
		if err != nil {
			return out, fmt.Errorf("bobswatches fetch: %w", err)
		}
		if status == 404 {
			return out, fmt.Errorf("bobswatches: no catalog page for brand %q", brand)
		}
		if status != 200 {
			return out, fmt.Errorf("bobswatches returned HTTP %d", status)
		}

		fresh := 0
		for _, tree := range ExtractJSONLD(body) {
			WalkJSON(tree, func(node map[string]any) {
				if derefString(node["@type"]) != "Product" || len(out) >= maxPerModel {
					return
				}
				title := derefString(node["name"])
				u := derefString(node["url"])
				ref := derefString(node["mpn"])
				var price float64
				currency := "USD"
				// offers may be a single object or an array — Bob's uses object
				switch om := node["offers"].(type) {
				case map[string]any:
					price = ParseLocalePrice(om["price"])
					currency = strings.ToUpper(derefString(om["priceCurrency"]))
				case []any:
					if len(om) > 0 {
						if first, ok := om[0].(map[string]any); ok {
							price = ParseLocalePrice(first["price"])
							currency = strings.ToUpper(derefString(first["priceCurrency"]))
						}
					}
				}
				color := derefString(node["color"])
				matText := title + " " + color
				img := derefString(node["image"])

				if title == "" || u == "" || price <= 0 {
					return
				}
				l := wfmodel.Listing{
					Source:   b.ID(),
					NativeID: NativeIDFromURL(u),
					Title:    title,
					URL:      u,
					Ref:      ref,
					Brand:    brand,
					Dial:     attrs.DetectDial(title),
					Material: attrs.DetectMaterial(matText),
					Scope:    attrs.DetectScope(title),
					Price:    price,
					Currency: currency,
					Dealer:   b.Name(),
					ImageURL: img,
				}
				if !matchesQuery(brand, model, &l) || seen[l.Key()] {
					return
				}
				seen[l.Key()] = true
				fresh++
				out = append(out, l)
			})
		}
		if fresh == 0 {
			break
		}
	}
	return out, nil
}
