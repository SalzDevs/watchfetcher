package sources

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"watchfetcher/internal/crawl"
	"watchfetcher/internal/httpclient"
	wfmodel "watchfetcher/internal/model"
)

// WatchfinderSource: server-rendered Magento catalogsearch results.
// Product cards carry structured data attributes (brand/series/model=ref/sku)
// plus schema.org name/url metas. Prices in GBP.
type WatchfinderSource struct {
	MaxPages int
}

const wfBase = "https://www.watchfinder.co.uk"

func (w *WatchfinderSource) ID() string   { return "watchfinder" }
func (w *WatchfinderSource) Name() string { return "Watchfinder" }

type watchfinderCard struct {
	URL    string
	Title  string
	Price  float64
	Brand  string
	Series string
	Ref    string
	Image  string
}

var (
	wfCardRe = regexp.MustCompile(
		`(?s)<a\s+href="(https://www\.watchfinder\.co\.uk/watches/[^"]+)"\s+class="product-card"(.*?)</a>`)
	wfNameRe  = regexp.MustCompile(`itemprop="name"\s+content="([^"]*)"`)
	wfPriceRe = regexp.MustCompile(`class="price">\s*&#163;([0-9,]+)|class="price">\s*£([0-9,]+)`)
	wfImgRe   = regexp.MustCompile(`data-product-image="([^"]+)"`)

	wfAttr = func(block, name string) string {
		re := regexp.MustCompile(`data-product-` + name + `="([^"]*)"`)
		m := re.FindStringSubmatch(block)
		if m == nil {
			return ""
		}
		return htmlUnescape(m[1])
	}
)

var htmlEscapes = strings.NewReplacer(
	"&#x20;", " ", "&#x3A;", ":", "&#x2F;", "/", "&#x27;", "'", "&amp;", "&",
	"&#x2;", "", "&#xA;", " ",
)

func htmlUnescape(s string) string {
	return htmlEscapes.Replace(s)
}

func (w *WatchfinderSource) Fetch(ctx context.Context, client *httpclient.Client, brand, model string, maxPerModel int) ([]wfmodel.Listing, error) {
	q := strings.TrimSpace(brand + " " + model)
	var out []wfmodel.Listing
	seen := map[string]bool{}

	for page := 1; page <= w.MaxPages; page++ {
		pageParam := ""
		if page > 1 {
			pageParam = fmt.Sprintf("&p=%d", page)
		}
		u := wfBase + "/catalogsearch/result/?q=" + url.QueryEscape(q) + pageParam
		_, body, err := crawl.DoGetWithRetry(ctx, client, w.ID(), u, nil)
		if err != nil {
			return out, fmt.Errorf("watchfinder fetch: %w", err)
		}
		cards := parseWatchfinderCards(body)
		fresh := 0
		for _, c := range cards {
			if seen[c.URL] {
				continue
			}
			seen[c.URL] = true
			l := wfmodel.Listing{
				Source:   w.ID(),
				NativeID: NativeIDFromURL(c.URL),
				Title:    c.Title,
				URL:      c.URL,
				Ref:      c.Ref,
				Brand:    c.Brand,
				Model:    c.Series,
				Price:    c.Price,
				Currency: "GBP",
				Dealer:   w.Name(),
				ImageURL: c.Image,
			}
			if !matchesQuery(brand, model, &l) {
				continue
			}
			fresh++
			out = append(out, l)
			if len(out) >= maxPerModel {
				return out, nil
			}
		}
		if fresh == 0 {
			break
		}
	}
	return out, nil
}

func parseWatchfinderCards(html string) []watchfinderCard {
	var cards []watchfinderCard
	for _, m := range wfCardRe.FindAllStringSubmatch(html, -1) {
		block := htmlUnescape(m[2])
		titleM := wfNameRe.FindStringSubmatch(block)
		title := ""
		if titleM != nil {
			title = strings.TrimSpace(htmlUnescape(titleM[1]))
		} else {
			title = strings.TrimSpace(StripTags(block))
			if len(title) > 120 {
				title = title[:120]
			}
		}
		priceM := wfPriceRe.FindStringSubmatch(block)
		if title == "" || priceM == nil {
			continue
		}
		priceStr := priceM[1]
		if priceStr == "" {
			priceStr = priceM[2]
		}
		price, _ := strconv.ParseFloat(strings.ReplaceAll(priceStr, ",", ""), 64)
		if price <= 0 {
			continue
		}
		var img string
		if imgM := wfImgRe.FindStringSubmatch(htmlUnescape(block)); imgM != nil {
			img = imgM[1]
		}
		cards = append(cards, watchfinderCard{
			URL:    strings.Split(m[1], "?")[0],
			Title:  title,
			Price:  price,
			Brand:  wfAttr(m[2], "brand"),
			Series: wfAttr(m[2], "series"),
			Ref:    wfAttr(m[2], "model"),
			Image:  img,
		})
	}
	return cards
}
