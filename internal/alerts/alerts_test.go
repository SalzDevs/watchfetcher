package alerts

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"watchledger/internal/ledger"
	"watchledger/internal/money"
	"watchledger/internal/store"
)

type captureMailer struct {
	Emails []string
}

func (m *captureMailer) SendLoginLink(email, link string) error { return nil }
func (m *captureMailer) SendAlert(email, subject, body string) error {
	m.Emails = append(m.Emails, email+"|"+subject)
	return nil
}

func fixtureDB(t *testing.T) *sql.DB {
	db, err := store.Open("file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAlertsTriggerAndCap(t *testing.T) {
	db := fixtureDB(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	db.Exec(`INSERT OR IGNORE INTO sources (id, name, access_status, rights_basis, rights_reviewed_at, reviewer, enabled)
		VALUES ('auction_a', 'a', 'approved', 'test', strftime('%s','now'), 't', 1)`)
	db.Exec(`INSERT INTO users (id, email) VALUES (1, 'u@b.com')`)
	db.Exec(`INSERT INTO watchlist (user_id, ref) VALUES (1, '126610LN')`)

	// initial evidence: 4 comps at t0
	seed := func(price string, at time.Time) {
		if _, err := ledger.AppendObservation(db, ledger.Observation{
			SourceID: "auction_a", Kind: "auction_realised", Ref: "126610LN",
			Brand: "Rolex", Model: "Submariner Date", Title: "comp",
			Price: price, Currency: "USD", PriceUSD: price, ObservedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	seed("13800", now.Add(-48*time.Hour))
	seed("13900", now.Add(-47*time.Hour))
	seed("14000", now.Add(-46*time.Hour))
	seed("14100", now.Add(-45*time.Hour))

	mailer := &captureMailer{}
	res, err := Run(db, mailer, "http://localhost:8080", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 {
		t.Fatalf("first run must fire (new evidence since last_alerted=0), got %+v", res)
	}
	if !strings.Contains(mailer.Emails[0], "u@b.com") {
		t.Fatalf("wrong recipient: %v", mailer.Emails)
	}

	// nothing new → nothing sent
	res, err = Run(db, mailer, "http://localhost:8080", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 0 {
		t.Fatalf("nothing new must send nothing, got %+v", res)
	}

	// new comp arrives → fires again
	seed("14200", now.Add(1*time.Hour))
	res, err = Run(db, mailer, "http://localhost:8080", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 {
		t.Fatalf("new evidence must fire, got %+v", res)
	}
}

// a watched ref with zero observations never fires
func TestAlertsNoEvidenceNoNoise(t *testing.T) {
	db := fixtureDB(t)
	now := time.Now().UTC()
	db.Exec(`INSERT INTO users (id, email) VALUES (1, 'x@b.com')`)
	db.Exec(`INSERT INTO watchlist (user_id, ref) VALUES (1, '999999')`)
	mailer := &captureMailer{}
	res, err := Run(db, mailer, "http://localhost:8080", now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 0 {
		t.Fatalf("no evidence must send nothing, got %+v", res)
	}
}

var _ = money.Zero
