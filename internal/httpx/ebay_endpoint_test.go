package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// eBay production key-set requirement: challenge echo verbatim, deletion
// notifications archived (G3) + acknowledged.
func TestEbayNotificationEndpoint(t *testing.T) {
	srv := testServer(t)
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/ebay/notifications?verification_challenge=p0RnF7CjaQ%3D%3D", nil))
	if w.Code != 200 || w.Body.String() != "p0RnF7CjaQ==" {
		t.Fatalf("challenge echo broken: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications", strings.NewReader(`{"notification":{"data":{"userId":"x1"}}}`)))
	if w.Code != 200 {
		t.Fatalf("deletion POST: %d", w.Code)
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

// shared verification token: portal value must be in the payload when configured
func TestEbayVerificationToken(t *testing.T) {
	srv := testServer(t)
	t.Setenv("EBAY_VERIFICATION_TOKEN", "tok123")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications", strings.NewReader(`{"notification":{"verificationToken":"tok123"}}`)))
	if w.Code != 200 {
		t.Fatalf("matching token must pass: %d", w.Code)
	}
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/ebay/notifications", strings.NewReader(`{"notification":{"verificationToken":"forged"}}`)))
	if w.Code != 401 {
		t.Fatalf("forged token must 401: %d", w.Code)
	}
}
