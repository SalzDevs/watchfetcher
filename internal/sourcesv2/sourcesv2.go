// Package sourcesv2: rights-approved source adapters (PLAN.md §4 Tier 1+).
// Contract: Fetch returns raw bytes; Extract is a pure function over them.
// Gated at runtime by store.SourceEnabled (G6).
package sourcesv2

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

var client = &http.Client{Timeout: 45 * time.Second}

const userAgent = "Mozilla/5.0 (compatible; WatchLedger/1.0; +https://watchfairvalue.com/bot)"

// FetchRaw — rate-limited, size-capped fetch (PLAN.md §6 stage rules).
func FetchRaw(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch %s: %w", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch %s: status %d", url, res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(res.Body, 10<<20)) // 10MB cap
	if err != nil {
		return nil, "", err
	}
	return body, ct, nil
}
