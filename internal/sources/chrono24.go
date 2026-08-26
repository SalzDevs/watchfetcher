package sources

import (
	"context"
	"regexp"
	"fmt"
	"net/url"
	"strings"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/httpclient"
	wfmodel "watchfetcher/internal/model"
)

// Chrono24Source: JSON-LD AggregateOffer parsing on search pages.
// Requires Chrome-impersonating TLS (the client wrapper handles this).
// One fetch per (brand, model) covers every dial variant via titles.
type Chrono24Source struct {
	MaxPages int
}

func (c *Chrono24Source) ID() string   { return "chrono24" }
func (c *Chrono24Source) Name() string { return "Chrono24" }

func (c *Chrono24Source) searchURL(terms string, page int) string {
	encoded := url.QueryEscape(strings.TrimSpace(terms))
	return fmt.Sprintf(
		"https://www.chrono24.com/search/index.htm?dosearch=true&query=%s&pageSize=120&page=%d",
		encoded, page,
	)
}

func (c *Chrono24Source) Fetch(ctx context.Context, client *httpclient.Client, brand, model string, maxPerModel int) ([]wfmodel.Listing, error) {
	var out []wfmodel.Listing
	seen := map[string]bool{}
	maxPages := (maxPerModel + 119) / 120
	if maxPages < 1 {
		maxPages = 1
	}
	terms := strings.TrimSpace(brand + " " + model)

	for page := 1; page <= maxPages; page++ {
		status, body, err := client.Get(ctx, c.searchURL(terms, page))
		if err != nil {
			return out, fmt.Errorf("chrono24 fetch: %w", err)
		}
		if status != 200 {
			return out, fmt.Errorf("chrono24 returned HTTP %d", status)
		}
		offers := CollectOffers(ExtractJSONLD(body))
		fresh := 0
		for _, offer := range offers {
			l := c.offerToListing(offer)
			if l == nil || seen[l.Key()] {
				continue
			}
			if !matchesQuery(brand, model, l) {
				continue
			}
			seen[l.Key()] = true
			fresh++
			out = append(out, *l)
			if len(out) >= maxPerModel {
				return out, nil
			}
		}
		if fresh == 0 {
			break // last page
		}
	}
	return out, nil
}

func (c *Chrono24Source) offerToListing(offer map[string]any) *wfmodel.Listing {
	u := derefString(offer["url"])
	title := derefString(offer["name"])
	price := ParseLocalePrice(offer["price"])
	currency := strings.ToUpper(derefString(offer["priceCurrency"]))
	if currency == "" {
		currency = "USD"
	}
	if u == "" || title == "" || price <= 0 {
		return nil
	}
	img := ""
	if rawImg, ok := offer["image"]; ok {
		switch v := rawImg.(type) {
		case string:
			img = v
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					img = s
				} else if m, ok := v[0].(map[string]any); ok {
					img = derefString(m["contentUrl"])
				}
			}
		}
	}
	nativeID := ""
	if m := regexp.MustCompile(`--id(\d+)`).FindStringSubmatch(u); m != nil {
		nativeID = m[1]
	}
	ref := attrs.ExtractRef(u)
	if ref == "" {
		ref = attrs.ExtractRef(title)
	}
	return &wfmodel.Listing{
		Source:   c.ID(),
		NativeID: nativeID,
		Title:    title,
		URL:      u,
		Ref:      ref,
		Brand:    brandName(offer),
		Dial:     attrs.DetectDial(title),
		Material: attrs.DetectMaterial(title),
		Price:    price,
		Currency: currency,
		ImageURL: img,
	}
}

func brandName(offer map[string]any) string {
	if b, ok := offer["brand"].(map[string]any); ok {
		return derefString(b["name"])
	}
	return derefString(offer["brand"])
}
