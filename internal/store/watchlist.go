package store

import (
	"database/sql"
	"time"
)

// ---- watchlist (Phase 5: the unit of attention is the reference) ----

type WatchRow struct {
	Ref         string
	LastAlerted int64
}

func WatchlistFor(db *sql.DB, userID int64) ([]WatchRow, error) {
	rows, err := db.Query(`SELECT ref, last_alerted FROM watchlist WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchRow
	for rows.Next() {
		var w WatchRow
		if err := rows.Scan(&w.Ref, &w.LastAlerted); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func WatchAdd(db *sql.DB, userID int64, ref string) error {
	_, err := db.Exec(`INSERT OR IGNORE INTO watchlist (user_id, ref) VALUES (?, ?)`, userID, ref)
	return err
}

func WatchRemove(db *sql.DB, userID int64, ref string) error {
	_, err := db.Exec(`DELETE FROM watchlist WHERE user_id = ? AND ref = ?`, userID, ref)
	return err
}

func Watching(db *sql.DB, userID int64, ref string) bool {
	var one int
	err := db.QueryRow(`SELECT 1 FROM watchlist WHERE user_id = ? AND ref = ?`, userID, ref).Scan(&one)
	return err == nil
}

func MarkAlerted(db *sql.DB, userID int64, ref string, at time.Time) error {
	_, err := db.Exec(`UPDATE watchlist SET last_alerted = ? WHERE user_id = ? AND ref = ?`, at.Unix(), userID, ref)
	return err
}

// AllWatchers — for the alert job.
func AllWatchers(db *sql.DB) ([]struct {
	UserID int64
	Ref    string
	Last   int64
}, error) {
	rows, err := db.Query(`SELECT user_id, ref, last_alerted FROM watchlist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		UserID int64
		Ref    string
		Last   int64
	}
	for rows.Next() {
		var r struct {
			UserID int64
			Ref    string
			Last   int64
		}
		if err := rows.Scan(&r.UserID, &r.Ref, &r.Last); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- price reports (Tier 3: review before ledger) ----

type PriceReport struct {
	ID       int64
	UserID   int64
	Ref      string
	Price    string
	Currency string
	PaidAt   int64
	Status   string
}

func AddPriceReport(db *sql.DB, userID int64, ref, price, currency string, paidAt time.Time) (int64, error) {
	res, err := db.Exec(`
		INSERT INTO price_reports (user_id, ref, price, currency, paid_at)
		VALUES (?, ?, ?, ?, ?)`, userID, ref, price, currency, paidAt.Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func OpenPriceReports(db *sql.DB) ([]PriceReport, error) {
	rows, err := db.Query(`
		SELECT id, user_id, ref, price, currency, paid_at, status
		FROM price_reports WHERE status = 'submitted' ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PriceReport
	for rows.Next() {
		var r PriceReport
		if err := rows.Scan(&r.ID, &r.UserID, &r.Ref, &r.Price, &r.Currency, &r.PaidAt, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func SetReportStatus(db *sql.DB, id int64, status, reviewer string) error {
	_, err := db.Exec(`UPDATE price_reports SET status = ?, reviewed_by = ?, reviewed_at = strftime('%s','now') WHERE id = ?`,
		status, reviewer, id)
	return err
}
