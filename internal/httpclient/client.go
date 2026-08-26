// Package httpclient wraps a Chrome-impersonating TLS client.
// Chrono24 (and several other marketplaces) fingerprint TLS handshakes and
// block non-browser clients — a standard net/http transport gets 403'd.
package httpclient

import (
	"context"
	"io"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// Client is a Chrome-impersonating HTTP client with cookie jar.
type Client struct {
	inner tls_client.HttpClient
}

// New creates a Chrome-124-impersonating client.
func New() (*Client, error) {
	jar := tls_client.NewCookieJar()
	inner, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithClientProfile(profiles.Chrome_124),
		tls_client.WithCookieJar(jar),
		tls_client.WithTimeoutSeconds(30),
	)
	if err != nil {
		return nil, err
	}
	return &Client{inner: inner}, nil
}

func baseHeaders() http.Header {
	h := http.Header{}
	h.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	h.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8,application/json")
	h.Set("accept-language", "en-US,en;q=0.9")
	return h
}

// Get performs a GET with browser headers. Returns (status, body, error).
func (c *Client) Get(ctx context.Context, url string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	for k, vs := range baseHeaders() {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := c.inner.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return int(resp.StatusCode), "", err
	}
	return int(resp.StatusCode), string(body), nil
}

// PostJSON performs a POST with a JSON body and returns (status, body, error).
func (c *Client) PostJSON(ctx context.Context, url string, extraHeaders map[string]string, jsonBody string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(jsonBody))
	if err != nil {
		return 0, "", err
	}
	for k, vs := range baseHeaders() {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.inner.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return int(resp.StatusCode), "", err
	}
	return int(resp.StatusCode), string(body), nil
}

var _ = time.Second

// GetNoRedirect performs a GET without following redirects. Returns
// (status, locationHeader, body, error) — used for OAuth code capture.
func (c *Client) GetNoRedirect(ctx context.Context, url string) (int, string, string, error) {
	noFollow, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithClientProfile(profiles.Chrome_124),
		tls_client.WithCookieJar(tls_client.NewCookieJar()),
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithNotFollowRedirects(),
	)
	if err != nil {
		return 0, "", "", err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, "", "", err
	}
	for k, vs := range baseHeaders() {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return int(resp.StatusCode), resp.Header.Get("Location"), "", nil
}

// GetWithHeaders performs a GET with additional headers (e.g. Authorization).
func (c *Client) GetWithHeaders(ctx context.Context, url string, extra map[string]string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	for k, vs := range baseHeaders() {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.inner.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return int(resp.StatusCode), "", err
	}
	return int(resp.StatusCode), string(body), nil
}

// PostForm performs a POST with an x-www-form-urlencoded body.
// Some APIs (e.g. OAuth token endpoints) reject JSON bodies.
func (c *Client) PostForm(ctx context.Context, url string, extraHeaders map[string]string, formData string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(formData))
	if err != nil {
		return 0, "", err
	}
	for k, vs := range baseHeaders() {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.inner.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return int(resp.StatusCode), "", err
	}
	return int(resp.StatusCode), string(body), nil
}
