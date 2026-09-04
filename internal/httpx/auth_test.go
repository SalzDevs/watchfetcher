package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"watchledger/internal/auth"
)

// Magic-link flow end-to-end: start → link (captured by LogMailer) → confirm →
// session cookie → watchlist access. No password ever exists.
func TestAuthFlowE2E(t *testing.T) {
	srv := testServer(t)

	// anonymous: watchlist redirects to login
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/watchlist", nil))
	if w.Code != 303 {
		t.Fatalf("anonymous watchlist must redirect to login, got %d", w.Code)
	}

	// start login
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, postForm("/login", "email=a@b.com&next=/watchlist"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Link sent") {
		t.Fatalf("login start broken: %d", w.Code)
	}

	// dev mailer captured the link — extract the token
	lm := srv.Mailer.(*auth.LogMailer)
	link := lm.Last
	if !strings.Contains(link, "/login/confirm?token=") {
		t.Fatalf("no login link mailed: %q", link)
	}
	token := tokenFromLink(link)

	// confirm → session cookie
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/login/confirm?token="+token, nil))
	if w.Code != 303 {
		t.Fatalf("confirm: %d", w.Code)
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.HasPrefix(cookie, "wl_session=") {
		t.Fatalf("no session cookie: %q", cookie)
	}
	sess := cookie[:strings.Index(cookie, ";")]
	sess = strings.TrimPrefix(sess, "wl_session=")

	// token single-use
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/login/confirm?token="+token, nil))
	if !strings.Contains(w.Body.String(), "invalid or already used") {
		t.Fatal("login token must be single-use")
	}

	// session works: watchlist renders
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/watchlist", nil)
	req.Header.Set("Cookie", "wl_session="+sess)
	srv.Routes().ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Your watchlist") {
		t.Fatalf("session watchlist: %d", w.Code)
	}
}

// watchlist add → page shows ref → remove.
func TestWatchlistCRUD(t *testing.T) {
	srv := testServer(t)
	sess := loginAs(t, srv, "w@b.com")

	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedPost(sess, "/watchlist/add", "ref=126610LN"))
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedGet(sess, "/watchlist"))
	if !strings.Contains(w.Body.String(), "126610LN") {
		t.Fatal("watched ref missing from watchlist")
	}

	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedPost(sess, "/watchlist/remove", "ref=126610LN"))
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedGet(sess, "/watchlist"))
	if strings.Contains(w.Body.String(), "126610LN") {
		t.Fatal("removed ref must leave the watchlist")
	}
}

// price report: submit → admin verify → user_reported observation in ledger.
func TestPriceReportGate(t *testing.T) {
	srv := testServer(t)
	sess := loginAs(t, srv, "r@b.com")

	// anonymous admin is 404
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/admin/reports", nil))
	if w.Code != 404 {
		t.Fatal("admin must 404 for anonymous")
	}

	// submit a report
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedPost(sess, "/reports/submit", "ref=126610LN&price=13900&currency=USD&paid_at=2026-08-20"))
	if w.Code != 303 {
		t.Fatalf("report submit: %d", w.Code)
	}

	// submitted ≠ ledger: nothing in observations yet
	var n int
	srv.DB.QueryRow(`SELECT COUNT(*) FROM observations WHERE kind='user_reported'`).Scan(&n)
	if n != 0 {
		t.Fatal("unverified report must NOT enter the ledger")
	}

	// admin verify → ledger
	setAdmin(t, "admin@b.com")
	adminSess := loginAs(t, srv, "admin@b.com")
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedGet(adminSess, "/admin/reports"))
	if !strings.Contains(w.Body.String(), "13900") {
		t.Fatalf("admin queue missing report")
	}
	var reportID int64
	srv.DB.QueryRow(`SELECT id FROM price_reports WHERE status='submitted'`).Scan(&reportID)
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedPost(adminSess, "/admin/reports/verify", fmt.Sprintf("id=%d&action=verify", reportID)))

	srv.DB.QueryRow(`SELECT COUNT(*) FROM observations WHERE kind='user_reported' AND ref='126610LN'`).Scan(&n)
	if n != 1 {
		t.Fatalf("verified report must be in the ledger, got %d", n)
	}
}

// non-admin email → admin 404
func TestAdminGate(t *testing.T) {
	srv := testServer(t)
	setAdmin(t, "boss@b.com")
	sess := loginAs(t, srv, "notboss@b.com")
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, authedGet(sess, "/admin/reports"))
	if w.Code != 404 {
		t.Fatalf("non-admin must 404, got %d", w.Code)
	}
}

// ---- helpers ----

// testServer variant with the capturing mailer + admin env control
type LogMailerShim = auth.LogMailer

func setAdmin(t *testing.T, e string) {
	t.Setenv("ADMIN_EMAIL", e)
}

func loginAs(t *testing.T, srv *Server, email string) string {
	t.Helper()
	w := httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, postForm("/login", "email="+email))
	lm := srv.Mailer.(*LogMailerShim)
	token := tokenFromLink(lm.Last)
	w = httptest.NewRecorder()
	srv.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/login/confirm?token="+token, nil))
	cookie := w.Header().Get("Set-Cookie")
	return strings.TrimPrefix(strings.Split(cookie, ";")[0], "wl_session=")
}

func tokenFromLink(link string) string {
	i := strings.Index(link, "token=")
	if i < 0 {
		return ""
	}
	return link[i+len("token="):]
}

func authedGet(sess, path string) *http.Request        { return authedReq(sess, "GET", path, "") }
func authedPost(sess, path, form string) *http.Request { return authedReq(sess, "POST", path, form) }

func authedReq(sess, method, path, form string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if form != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Cookie", "wl_session="+sess)
	return req
}

func postForm(path, form string) *http.Request {
	req := httptest.NewRequest("POST", path, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}
