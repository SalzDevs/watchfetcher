package sources

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"watchfetcher/internal/attrs"
	"watchfetcher/internal/httpclient"
	wfmodel "watchfetcher/internal/model"
)

// The1916CompanySource: Salesforce B2C Commerce API through their own proxy.
// Guest SLAS PKCE flow (no secret) -> product-search -> batch products for
// canonical URLs and structured attributes (year/ref/material/dial color).
type The1916CompanySource struct {
	MaxPages int
}

func (t *The1916CompanySource) ID() string   { return "the1916company" }
func (t *The1916CompanySource) Name() string { return "The 1916 Company" }

const (
	tccProxy    = "https://www.the1916company.com/mobify/proxy/api"
	tccClientID = "3fd705df-2b7b-4a53-8a6c-75f19ce82f96" // their public storefront client
	tccOrg      = "f_ecom_bdcc_prd"
	tccSite     = "ns-company"
	tccCallback = "https://www.the1916company.com/callback"
)

var (
	tccTokenMu sync.Mutex
	tccToken   string
	tccTokenOK bool
	tccTokenAt time.Time
)

func (t *The1916CompanySource) guestToken(ctx context.Context, client *httpclient.Client) (string, error) {
	tccTokenMu.Lock()
	defer tccTokenMu.Unlock()
	if tccTokenOK && time.Since(tccTokenAt) < 25*time.Minute {
		return tccToken, nil
	}
	verifier := base64.RawURLEncoding.EncodeToString(randBytes(64))
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	authURL := fmt.Sprintf(
		"%s/shopper/auth/v1/organizations/%s/oauth2/authorize?redirect_uri=%s&response_type=code&client_id=%s&hint=guest&channel_id=%s&code_challenge=%s",
		tccProxy, tccOrg, url.QueryEscape(tccCallback), tccClientID, tccSite, challenge,
	)
	status, location, _, err := client.GetNoRedirect(ctx, authURL)
	if err != nil {
		return "", fmt.Errorf("1916 authorize: %w", err)
	}
	if status != 303 || !strings.Contains(location, "code=") {
		return "", fmt.Errorf("1916 guest auth failed (HTTP %d)", status)
	}
	code := location
	if i := strings.Index(code, "code="); i >= 0 {
		code = code[i+5:]
	}
	if j := strings.Index(code, "&"); j >= 0 {
		code = code[:j]
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code_pkce")
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("client_id", tccClientID)
	form.Set("channel_id", tccSite)
	form.Set("redirect_uri", tccCallback)
	status, respBody, err := client.PostForm(ctx,
		fmt.Sprintf("%s/shopper/auth/v1/organizations/%s/oauth2/token", tccProxy, tccOrg),
		map[string]string{"Accept": "application/json"}, form.Encode())
	if err != nil || status != 200 {
		return "", fmt.Errorf("1916 token exchange failed (HTTP %d)", status)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil || parsed.AccessToken == "" {
		return "", fmt.Errorf("1916 token parse failed")
	}
	tccToken = parsed.AccessToken
	tccTokenAt = time.Now()
	return tccToken, nil
}

type tccHit struct {
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	Price       float64 `json:"price"`
	Currency    string  `json:"currency"`
	CBrand      string  `json:"c_brand"`
	CRef        string  `json:"c_baseRefNum"`
	CYear       any     `json:"c_WatchYear"`
	CMaterial   string  `json:"c_material"`
	Image       struct {
		Link string `json:"link"`
	} `json:"image"`
}

type tccProduct struct {
	ID      string `json:"id"`
	SlugURL string `json:"slugUrl"`
	CDial   string `json:"c_dialColor"`
}

func (t *The1916CompanySource) getJSON(ctx context.Context, client *httpclient.Client, u string, headers map[string]string) (int, string, error) {
	return client.GetWithHeaders(ctx, u, headers)
}

func (t *The1916CompanySource) Fetch(ctx context.Context, client *httpclient.Client, brand, model string, maxPerModel int) ([]wfmodel.Listing, error) {
	token, err := t.guestToken(ctx, client)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json"}
	q := strings.TrimSpace(brand + " " + model)
	maxPages := (maxPerModel + 47) / 48
	if maxPages < 1 {
		maxPages = 1
	}

	var out []wfmodel.Listing
	seen := map[string]bool{}

	for page := 0; page < maxPages; page++ {
		searchURL := fmt.Sprintf(
			"%s/search/shopper-search/v1/organizations/%s/product-search?siteId=%s&q=%s&limit=48&offset=%d",
			tccProxy, tccOrg, tccSite, url.QueryEscape(q), page*48,
		)
		status, body, err := t.getJSON(ctx, client, searchURL, headers)
		if err != nil {
			return out, fmt.Errorf("1916 search: %w", err)
		}
		if status != 200 {
			return out, fmt.Errorf("1916 returned HTTP %d", status)
		}
		var payload struct {
			Hits []tccHit `json:"hits"`
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			return out, fmt.Errorf("1916 search parse: %w", err)
		}
		if len(payload.Hits) == 0 {
			break
		}

		// batch product details: canonical slugUrl + dial color
		ids := make([]string, 0, len(payload.Hits))
		for _, h := range payload.Hits {
			ids = append(ids, h.ProductID)
		}
		details := map[string]tccProduct{}
		for i := 0; i < len(ids); i += 20 {
			chunk := ids[i:]
			if len(chunk) > 20 {
				chunk = chunk[:20]
			}
			prodURL := fmt.Sprintf(
				"%s/product/shopper-products/v1/organizations/%s/products?ids=%s&siteId=%s",
				tccProxy, tccOrg, strings.Join(chunk, ","), tccSite,
			)
			pstatus, pbody, perr := t.getJSON(ctx, client, prodURL, headers)
			if perr != nil || pstatus != 200 {
				continue
			}
			var products struct {
				Data []tccProduct `json:"data"`
			}
			if err := json.Unmarshal([]byte(pbody), &products); err == nil {
				for _, p := range products.Data {
					details[p.ID] = p
				}
			}
		}

		fresh := 0
		for _, h := range payload.Hits {
			det := details[h.ProductID]
			u := det.SlugURL
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			brandName := h.CBrand
			title := strings.TrimSpace(brandName + " " + h.ProductName)
			year := 0
			if y, ok := h.CYear.(float64); ok {
				year = int(y)
			}
			ref := strings.TrimSpace(h.CRef)
			if i := strings.Index(ref, " "); i > 0 {
				ref = ref[:i] // '14060 BLK OYS' -> '14060'
			}
			dial := strings.ToLower(strings.TrimSpace(det.CDial))
			if dial == "" {
				dial = attrs.DetectDial(title)
			}
			l := wfmodel.Listing{
				Source:   t.ID(),
				NativeID: h.ProductID,
				Title:    title,
				URL:      u,
				Ref:      ref,
				Brand:    brandName,
				Model:    h.ProductName,
				Dial:     dial,
				Material: attrs.DetectMaterial(title + " " + h.CMaterial),
				Year:     year,
				Price:    h.Price,
				Currency: strings.ToUpper(h.Currency),
				Dealer:   t.Name(),
				ImageURL: h.Image.Link,
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
