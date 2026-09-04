// Package alerts: the outbound retention loop (PLAN.md §11: retention must be
// outbound). Trigger = something new on a watched reference since the last
// alert. Frequency is naturally capped by data cadence; one email per run
// per user, everything links back to the reference page.
package alerts

import (
	"database/sql"
	"fmt"
	"time"

	"watchledger/internal/auth"
	"watchledger/internal/store"
)

type Result struct {
	Sent     int
	Skipped  int
	Watchers int
}

// Run — check every watch, send where there is genuinely something new.
// The trigger set (PLAN.md §5.3): new exact-tier realised comps, gate flips.
// Simple honest rule: anything new on the reference since last_alerted fires;
// nothing new = nothing sent (no marketing emails, ever).
func Run(db *sql.DB, mailer auth.Mailer, baseURL string, now time.Time) (Result, error) {
	var res Result

	watchers, err := store.AllWatchers(db)
	if err != nil {
		return res, err
	}
	res.Watchers = len(watchers)

	emails := map[int64]string{}
	userEmail := func(userID int64) string {
		if e, ok := emails[userID]; ok {
			return e
		}
		var e string
		db.QueryRow(`SELECT email FROM users WHERE id = ?`, userID).Scan(&e)
		emails[userID] = e
		return e
	}

	for _, w := range watchers {
		// new ledger activity strictly after the last alert
		newSince := 0
		db.QueryRow(`SELECT COUNT(*) FROM observations WHERE UPPER(ref) = UPPER(?) AND observed_at > ?`,
			w.Ref, w.Last).Scan(&newSince)
		if newSince == 0 {
			res.Skipped++
			continue
		}

		email := userEmail(w.UserID)
		if email == "" {
			res.Skipped++
			continue
		}

		subject := fmt.Sprintf("WatchLedger — new evidence on %s", w.Ref)
		body := fmt.Sprintf(
			"%d new observation(s) on %s since your last alert.\n\nSee the evidence: %s/references/%s\n\n"+
				"You get this because you watch %s. One-click removal: %s (reply STOP and we'll remove it by hand — we're small).\n",
			newSince, w.Ref, baseURL, w.Ref, w.Ref, baseURL)

		if err := mailer.SendAlert(email, subject, body); err != nil {
			return res, fmt.Errorf("alert %s: %w", email, err)
		}
		if err := store.MarkAlerted(db, w.UserID, w.Ref, now); err != nil {
			return res, err
		}
		res.Sent++
	}
	return res, nil
}
