// eBay Browse API client (PLAN.md §4 Tier 2 — official ToS, no scraping).
// Credentials via env EBAY_CLIENT_ID / EBAY_CLIENT_SECRET (client-credentials
// OAuth). Missing credentials = ErrNoCredentials: the pipeline degrades
// honestly, never silently (G9).
package sourcesv2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoCredentials = errors.New("eBay: EBAY_CLIENT_ID / EBAY_CLIENT_SECRET not set")
	ErrNoItems       = errors.New("eBay: no items in response")
)

const (
	ebayTokenURL  = "https://api.ebay.com/identity/v1/oauth2/token"
	ebaySearchURL = "https://api.ebay.com/buy/browse/v1/item_summary/search"
	ebayScope     = "https://api.ebay.com/oauth/api_scope"
)

// eBayClient — token cache built in; safe for concurrent use.
type eBayClient struct {
	ID     string
	Secret string

	mu    sync.Mutex
	token string
	exp   time.Time
}

func NewEBayClientFromEnv(id, secret string) *eBayClient {
	return &eBayClient{ID: id, Secret: secret}
}

func (c *eBayClient) tokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp.Add(-5*time.Minute)) {
		return c.token, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {ebayScope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ebayTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.ID, c.Secret)
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("eBay token: status %d", res.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", errors.New("eBay token: empty access_token")
	}
	c.token = tok.AccessToken
	c.exp = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.token, nil
}

// eBayItem is the subset of item_summary we ledger.
type EBayItem struct {
	ItemID      string    `json:"itemId"` // 'v1|123456789|0'
	Title       string    `json:"title"`
	Price       EBayMoney `json:"price"`
	ItemWebURL  string    `json:"itemWebURL"`
	Condition   string    `json:"condition"`
	ItemEndDate time.Time `json:"itemEndDate"`
}

type EBayMoney struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// Search — Browse API item_summary search. Raw JSON also returned for the
// raw-document store (G3).
func (c *eBayClient) Search(ctx context.Context, q string, limit int) ([]EBayItem, json.RawMessage, error) {
	if c.ID == "" || c.Secret == "" {
		return nil, nil, ErrNoCredentials
	}
	token, err := c.tokenFor(ctx)
	if err != nil {
		return nil, nil, err
	}
	u := fmt.Sprintf("%s?q=%s&limit=%d&filter=conditionIds:{3000},buyingOptions:{FIXED_PRICE}",
		ebaySearchURL, url.QueryEscape(q), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-EBAY-C-MARKETPLACE-ID", "EBAY_US")
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, nil, fmt.Errorf("eBay search: rate limited")
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("eBay search: status %d", res.StatusCode)
	}
	body, err := readAll(res)
	if err != nil {
		return nil, nil, err
	}
	var parsed struct {
		ItemSummaries []EBayItem `json:"itemSummaries"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, body, fmt.Errorf("eBay search parse: %w", err)
	}
	return parsed.ItemSummaries, json.RawMessage(body), nil
}

func readAll(res *http.Response) ([]byte, error) {
	defer res.Body.Close()
	return io.ReadAll(io.LimitReader(res.Body, 10<<20))
}

// CoreItemID strips eBay's 'v1|<id>|0' wrapper.
func CoreItemID(ebayItemID string) string {
	parts := strings.Split(ebayItemID, "|")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ebayItemID
}
