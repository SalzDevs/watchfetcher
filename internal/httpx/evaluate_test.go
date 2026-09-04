package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Editorial rules (PLAN.md §13): rendered briefs/evaluations carry position +
// evidence, never a quality judgement. Grepped in CI — the share vector can
// never ship a banned phrase.
func TestBriefEditorialRules(t *testing.T) {
	banned := []string{
		"undervalued", "overvalued", "bargain", "fair price", "fair-price",
		"overpriced", "underpriced", "steal", "good deal", "true value",
		"worth exactly", "is worth",
	}
	// exercise all three surfaces with a rich evaluation
	srv := testServer(t)

	for _, path := range []string{
		"/evaluate/1",
		"/evaluate/1/brief",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		srv.Routes().ServeHTTP(w, req)
		body := strings.ToLower(w.Body.String())
		for _, b := range banned {
			if strings.Contains(body, b) {
				t.Errorf("%s contains banned phrase %q — editorial rule violation", path, b)
			}
		}
	}
}

func TestEvaluateFlow(t *testing.T) {
	srv := testServer(t)

	// form renders
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/evaluate", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Evaluate a listing") {
		t.Fatalf("form broken: %d", w.Code)
	}

	// submit → redirect → permalink renders
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/evaluate", strings.NewReader(
		"ref=126610LN&price=14500&currency=USD&to_country=PT"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 303 {
		t.Fatalf("submit: want 303 got %d body=%.200s", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")

	// same inputs again → same permalink (content-addressed, G4)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/evaluate", strings.NewReader(
		"ref=126610LN&price=14500&currency=USD&to_country=PT"))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.Routes().ServeHTTP(w2, req2)
	if w2.Header().Get("Location") != loc {
		t.Fatalf("same inputs must give same permalink: %s vs %s", loc, w2.Header().Get("Location"))
	}

	// permalink: position + band + landed cost render
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", loc, nil))
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("permalink: %d", w.Code)
	}
	for _, want := range []string{"Within the realised band", "Landed cost", "Estimated landed cost"} {
		if !strings.Contains(body, want) {
			t.Errorf("permalink missing %q", want)
		}
	}

	// brief renders position + evidence, one-pager CSS present
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", loc+"/brief", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Evidence sheet") {
		t.Fatalf("brief broken: %d", w.Code)
	}

	// unknown permalink → 404
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/evaluate/99999", nil))
	if w.Code != 404 {
		t.Fatal("unknown evaluation must 404")
	}
}
