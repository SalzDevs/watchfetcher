package httpx

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strings"
	"testing"
)

// eBay production key-set requirement (spec): challenge_code → JSON
// challengeResponse = sha256(challenge_code + verificationToken + endpoint).
// Deletion notifications archived (G3) + acknowledged.
func TestEbayNotificationEndpoint(t *testing.T) {
	srv := testServer(t)
	t.Setenv("EBAY_VERIFICATION_TOKEN", "qK4sBjqSWp1WguF1uBe5FZkx622tUyH9zR2")

	challengeCode := "1234567"
	h := sha256.New()
	h.Write([]byte(challengeCode))
	h.Write([]byte("qK4sBjqSWp1WguF1uBe5FZkx622tUyH9zR2"))
	h.Write([]byte("https://watchfairvalue.com/ebay/notifications"))
	want := hex.EncodeToString(h.Sum(nil))

	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/ebay/notifications?challenge_code="+challengeCode, nil))
	if w.Code != 200 {
		t.Fatalf("challenge: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), want) {
		t.Fatalf("challengeResponse wrong:\nwant %s\ngot  %s", want, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type must be application/json, got %s", ct)
	}
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications",
		strings.NewReader(`{"metadata":{"topic":"MARKETPLACE_ACCOUNT_DELETION"},"notification":{"data":{"userId":"x1"}}}`)))
	if w.Code != 200 {
		t.Fatalf("deletion POST (no token in payload — spec): %d", w.Code)
	}
	var n int
	srv.DB.QueryRow(`SELECT COUNT(*) FROM raw_documents WHERE url='marketplace-account-deletion'`).Scan(&n)
	if n != 1 {
		t.Fatal("deletion payload must be archived (G3)")
	}
	// empty POST → 400
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications", strings.NewReader("")))
	if w.Code != 400 {
		t.Fatal("empty deletion payload must 400")
	}
}

// notifications are acknowledged unconditionally (spec) — authenticity via
// signature header is the hardening path, not the token.
func TestEbayNotificationAlwaysAcknowledged(t *testing.T) {
	srv := testServer(t)
	t.Setenv("EBAY_VERIFICATION_TOKEN", "tok123")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications", strings.NewReader(`{"notification":{"data":{"userId":"x"}}}`)))
	if w.Code != 200 {
		t.Fatalf("acknowledge must not depend on token: %d", w.Code)
	}
}
