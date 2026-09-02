// Bonhams adapter: published auction results (public record, PLAN.md §4 Tier 1).
//
// Page anatomy (fixture-verified): Next.js __NEXT_DATA__ JSON contains a feed
// of SOLD lots. Each lot dict: title, styledDescription, brand, lotNo.full,
// slug, lotId, auctionId, status ('SOLD'), currency.iso_code, hammerTime,
// price.hammerPrice/hammerPremium. hammerPrice 0 / status ≠ SOLD = skipped,
// never fabricated.
package sourcesv2

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"time"

	"watchledger/internal/money"
)

// Lot is one parsed auction result candidate.
type Lot struct {
	LotNumber     string
	LotID         string
	AuctionID     string
	Name          string // title
	Description   string // styledDescription, tags stripped
	URL           string
	Currency      string // ISO code
	HammerPrice   money.Decimal
	HammerPremium money.Decimal
	SaleDate      time.Time // hammer time, per lot
	Sold          bool
}

// Auction is one parsed sale page.
type Auction struct {
	ID       string
	SaleDate time.Time
	Lots     []Lot
}

var (
	reNEXTData  = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
	reStripTags = regexp.MustCompile(`<[^>]*>`)
)

// ExtractAuction — parse __NEXT_DATA__, walk the tree for sold-lot objects
// (identified by the lotItemId key), build candidates. Objects that fail to
// parse are skipped and counted — never guessed.
func ExtractAuction(raw []byte, auctionID string) (Auction, error) {
	a := Auction{ID: auctionID}

	m := reNEXTData.FindSubmatch(raw)
	if m == nil {
		return a, errors.New("__NEXT_DATA__ script not found — page format changed?")
	}
	var tree any
	if err := json.Unmarshal(m[1], &tree); err != nil {
		return a, fmt.Errorf("parse __NEXT_DATA__: %w", err)
	}

	seen := map[string]bool{}
	skipped := 0
	var walk func(node any)
	walk = func(node any) {
		switch n := node.(type) {
		case []any:
			for _, c := range n {
				walk(c)
			}
		case map[string]any:
			if _, isLot := n["lotItemId"]; isLot {
				lot, err := parseLot(n)
				if err != nil {
					skipped++
				} else if !seen[lot.LotID+"/"+lot.AuctionID] {
					seen[lot.LotID+"/"+lot.AuctionID] = true
					a.Lots = append(a.Lots, lot)
					if lot.SaleDate.After(a.SaleDate) {
						a.SaleDate = lot.SaleDate
					}
				}
				return // no nested lots
			}
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(tree)

	if len(a.Lots) == 0 {
		return a, fmt.Errorf("no lots extracted from auction %s (%d unparseable objects) — page format changed?", auctionID, skipped)
	}
	return a, nil
}

func parseLot(n map[string]any) (Lot, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return Lot{}, err
	}
	var rl struct {
		LotID  string `json:"lotId"`
		Slug   string `json:"slug"`
		Title  string `json:"title"`
		Brand  string `json:"brand"`
		Desc   string `json:"styledDescription"`
		Status string `json:"status"`
		Number struct {
			Full string `json:"full"`
		} `json:"lotNo"`
		AuctionID string `json:"auctionId"`
		Currency  struct {
			ISOCode string `json:"iso_code"`
		} `json:"currency"`
		HammerTime struct {
			Datetime string `json:"datetime"`
		} `json:"hammerTime"`
		Price struct {
			HammerPrice   float64 `json:"hammerPrice"`
			HammerPremium float64 `json:"hammerPremium"`
		} `json:"price"`
	}
	if err := json.Unmarshal(b, &rl); err != nil {
		return Lot{}, err
	}
	if rl.LotID == "" || rl.Number.Full == "" || rl.Title == "" {
		return Lot{}, fmt.Errorf("lot missing identity")
	}

	lot := Lot{
		LotNumber:   rl.Number.Full,
		LotID:       rl.LotID,
		AuctionID:   rl.AuctionID,
		Name:        html.UnescapeString(rl.Title),
		Description: html.UnescapeString(reStripTags.ReplaceAllString(rl.Desc, " ")),
		Currency:    rl.Currency.ISOCode,
	}
	if lot.Currency == "" {
		if ccy, ok := n["currency"].(map[string]any); ok {
			lot.Currency = symbolToCode[html.UnescapeString(fmt.Sprint(ccy["bonhams_code"]))]
		}
	}
	if rl.Slug != "" && rl.AuctionID != "" {
		lot.URL = fmt.Sprintf("https://www.bonhams.com/auction/%s/lot/%s/%s/", rl.AuctionID, rl.LotID, rl.Slug)
	}
	if t, err := time.Parse(time.RFC3339, rl.HammerTime.Datetime); err == nil {
		lot.SaleDate = t
	}
	if rl.Status == "SOLD" && rl.Price.HammerPrice > 0 {
		lot.Sold = true
		lot.HammerPrice = money.MustDecimal(fmt.Sprintf("%f", rl.Price.HammerPrice))
		lot.HammerPremium = money.MustDecimal(fmt.Sprintf("%f", rl.Price.HammerPremium))
	}
	return lot, nil
}

var symbolToCode = map[string]string{
	"€": "EUR", "£": "GBP", "$": "USD", "CHF": "CHF", "HK$": "HKD", "¥": "JPY",
}
