package crawl

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"watchfetcher/internal/httpclient"
)

// IsBannedStatus reports whether a status code indicates an IP ban / rate-limit.
func IsBannedStatus(status int) bool {
	return status == 403 || status == 429
}

// IsRetryable reports whether we should retry with backoff.
func IsRetryable(status int) bool {
	return status == 429 || status == 503 || status == 520 || status == 522 || status == 524
}

// ParseRetryAfter tries to extract wait duration from Retry-After header value.
// Accepts seconds ("120") or HTTP-date. Falls back to 0 if unparseable.
func ParseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// DoGetWithRetry performs a GET with polite rate-limiting, jitter, and
// exponential backoff for 429/503. It records HTTP metrics for ban monitoring.
// Returns last status, body, and error. Caller should treat 403 as banned.
func DoGetWithRetry(ctx context.Context, client *httpclient.Client, source, url string, m *Metrics) (int, string, error) {
	return doWithRetry(ctx, source, m, func() (int, string, string, error) {
		// Wait for polite interval before each attempt
		if err := Wait(ctx, source); err != nil {
			return 0, "", "", err
		}
		status, body, err := client.Get(ctx, url)
		return status, "", body, err
	})
}

// DoGetWithHeadersRetry is like DoGetWithRetry but with extra headers (e.g. Authorization).
func DoGetWithHeadersRetry(ctx context.Context, client *httpclient.Client, source, url string, headers map[string]string, m *Metrics) (int, string, error) {
	return doWithRetry(ctx, source, m, func() (int, string, string, error) {
		if err := Wait(ctx, source); err != nil {
			return 0, "", "", err
		}
		status, body, err := client.GetWithHeaders(ctx, url, headers)
		return status, "", body, err
	})
}

// DoGetNoRedirectRetry wraps GetNoRedirect with backoff. Unlike other
// helpers, HTTP 303 is success (OAuth code in Location) — not an error.
func DoGetNoRedirectRetry(ctx context.Context, client *httpclient.Client, source, url string, m *Metrics) (int, string, string, error) {
	m = globalOr(m)
	const maxRetries = 3
	var lastStatus int
	var lastLoc string
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := Wait(ctx, source); err != nil {
			return 0, "", "", err
		}
		status, loc, _, err := client.GetNoRedirect(ctx, url)
		lastStatus, lastLoc, lastErr = status, loc, err
		if m != nil && status != 0 {
			m.RecordHTTP(source, status)
		}
		if err != nil {
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(1000))*time.Millisecond
				select {
				case <-time.After(Jitter(backoff)):
				case <-ctx.Done():
					return lastStatus, lastLoc, "", ctx.Err()
				}
				continue
			}
			return lastStatus, lastLoc, "", lastErr
		}
		if IsBannedStatus(status) {
			if attempt == 0 && status == 429 {
				wait := 15*time.Second + time.Duration(rand.Intn(5000))*time.Millisecond
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return status, loc, "", ctx.Err()
				}
				continue
			}
			return status, loc, "", fmt.Errorf("%s returned HTTP %d (possible ban/rate-limit)", source, status)
		}
		if IsRetryable(status) {
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt)*2*time.Second + time.Duration(rand.Intn(2000))*time.Millisecond
				select {
				case <-time.After(Jitter(backoff)):
				case <-ctx.Done():
					return status, loc, "", ctx.Err()
				}
				continue
			}
			return status, loc, "", fmt.Errorf("%s returned HTTP %d after %d retries", source, status, maxRetries)
		}
		// 303 is success for OAuth; other 3xx/4xx/5xx (except 200/303) are errors
		if status == 303 || status == 200 {
			return status, loc, "", nil
		}
		return status, loc, "", fmt.Errorf("%s returned HTTP %d", source, status)
	}
	return lastStatus, lastLoc, "", lastErr
}

// DoPostFormRetry wraps PostForm.
func DoPostFormRetry(ctx context.Context, client *httpclient.Client, source, url string, headers map[string]string, form string, m *Metrics) (int, string, error) {
	return doWithRetry(ctx, source, m, func() (int, string, string, error) {
		if err := Wait(ctx, source); err != nil {
			return 0, "", "", err
		}
		status, body, err := client.PostForm(ctx, url, headers, form)
		return status, "", body, err
	})
}

func doWithRetry(ctx context.Context, source string, m *Metrics, do func() (int, string, string, error)) (int, string, error) {
	status, _, body, err := doWithRetryNoBody(ctx, source, m, do)
	return status, body, err
}

func doWithRetryNoBody(ctx context.Context, source string, m *Metrics, do func() (int, string, string, error)) (int, string, string, error) {
	m = globalOr(m)
	const maxRetries = 3
	var lastStatus int
	var lastBody string
	var lastLoc string
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		status, loc, body, err := do()
		lastStatus, lastLoc, lastBody, lastErr = status, loc, body, err

		if m != nil && status != 0 {
			m.RecordHTTP(source, status)
		}

		if err != nil {
			// Network error — retry with backoff unless context cancelled
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Intn(1000))*time.Millisecond
				select {
				case <-time.After(Jitter(backoff)):
				case <-ctx.Done():
					return lastStatus, lastLoc, lastBody, ctx.Err()
				}
				continue
			}
			return lastStatus, lastLoc, lastBody, lastErr
		}

		if IsBannedStatus(status) {
			// Don't hammer bans — one quick retry then give up and let circuit breaker open
			if attempt == 0 && status == 429 {
				// 429 may have Retry-After; wait a bit longer
				wait := 15*time.Second + time.Duration(rand.Intn(5000))*time.Millisecond
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return status, loc, body, ctx.Err()
				}
				continue
			}
			return status, loc, body, fmt.Errorf("%s returned HTTP %d (possible ban/rate-limit)", source, status)
		}

		if IsRetryable(status) {
			if attempt < maxRetries {
				// Respect Retry-After if we could parse it; else exponential
				backoff := time.Duration(1<<attempt)*2*time.Second + time.Duration(rand.Intn(2000))*time.Millisecond
				select {
				case <-time.After(Jitter(backoff)):
				case <-ctx.Done():
					return status, loc, body, ctx.Err()
				}
				continue
			}
			return status, loc, body, fmt.Errorf("%s returned HTTP %d after %d retries", source, status, maxRetries)
		}

		if status != 0 && status != 200 {
			// Non-retryable 4xx/5xx — fail fast, don't spam
			return status, loc, body, fmt.Errorf("%s returned HTTP %d", source, status)
		}

		return status, loc, body, nil
	}
	return lastStatus, lastLoc, lastBody, lastErr
}
