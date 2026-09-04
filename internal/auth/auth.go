// Package auth: magic-link accounts. No passwords in this DB, ever (PLAN.md §16 #7).
// Email delivery is behind Mailer — dev uses LogMailer (link to stdout);
// production plugs SMTP or a managed provider without touching call sites.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	SessionTTL = 30 * 24 * time.Hour
	LoginTTL   = 15 * time.Minute
)

// Mailer — the only outbound channel. Implementations: LogMailer (dev),
// SMTP/m managed provider (prod).
type Mailer interface {
	SendLoginLink(email, link string) error
	SendAlert(email, subject, body string) error
}

// LogMailer — dev: prints instead of sending. Never used in prod.
type LogMailer struct {
	Prefix string // e.g. "[dev-mail]"
	Last   string // last rendered link — test/dev convenience
}

func (m *LogMailer) SendLoginLink(email, link string) error {
	m.Last = link
	fmt.Printf("%s login for %s: %s\n", m.Prefix, email, link)
	return nil
}

func (m *LogMailer) SendAlert(email, subject, body string) error {
	fmt.Printf("%s alert for %s: %s\n%s\n", m.Prefix, email, subject, body)
	return nil
}

// NewLogMailer — dev/default mailer.
func NewLogMailer() *LogMailer { return &LogMailer{Prefix: "[dev-mail]"} }

// StartLogin — create a one-time login token (stored hashed), mail the link.
func StartLogin(db *sql.DB, mailer Mailer, email, baseURL string) error {
	email = normalize(email)
	if email == "" || !hasAt(email) {
		return fmt.Errorf("valid email required")
	}
	raw := newToken()
	sum := sha256.Sum256([]byte(raw))
	tok := hex.EncodeToString(sum[:])
	_, err := db.Exec(`INSERT INTO login_tokens (token, email, expires_at) VALUES (?, ?, ?)`,
		tok, email, time.Now().Add(LoginTTL).Unix())
	if err != nil {
		return err
	}
	return mailer.SendLoginLink(email, fmt.Sprintf("%s/login/confirm?token=%s", baseURL, raw))
}

// ConfirmLogin — consume the token, create the user (email IS the identity,
// no profile), mint a session. Returns the session token for the cookie.
func ConfirmLogin(db *sql.DB, rawToken string) (session string, err error) {
	sum := sha256.Sum256([]byte(rawToken))
	tok := hex.EncodeToString(sum[:])

	var email string
	var expires int64
	err = db.QueryRow(`SELECT email, expires_at FROM login_tokens WHERE token = ? AND used_at IS NULL`, tok).Scan(&email, &expires)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("login link invalid or already used")
	}
	if err != nil {
		return "", err
	}
	if time.Now().Unix() > expires {
		return "", fmt.Errorf("login link expired — request a new one")
	}

	// user upsert (email is the identity; nothing else about a user exists)
	if _, err := db.Exec(`INSERT OR IGNORE INTO users (email) VALUES (?)`, email); err != nil {
		return "", err
	}
	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID); err != nil {
		return "", err
	}

	if _, err := db.Exec(`UPDATE login_tokens SET used_at = strftime('%s','now') WHERE token = ?`, tok); err != nil {
		return "", err
	}

	session = newToken()
	sum2 := sha256.Sum256([]byte(session))
	if _, err := db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		hex.EncodeToString(sum2[:]), userID, time.Now().Add(SessionTTL).Unix()); err != nil {
		return "", err
	}
	return session, nil
}

// User — resolve the session cookie to (userID, email). No session = anonymous.
func User(db *sql.DB, sessionToken string) (int64, string, bool) {
	if sessionToken == "" {
		return 0, "", false
	}
	sum := sha256.Sum256([]byte(sessionToken))
	tok := hex.EncodeToString(sum[:])
	var userID int64
	var email string
	err := db.QueryRow(`
		SELECT u.id, u.email FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > strftime('%s','now')`, tok).Scan(&userID, &email)
	if err != nil {
		return 0, "", false
	}
	return userID, email, true
}

// Logout — consume the session.
func Logout(db *sql.DB, sessionToken string) {
	if sessionToken == "" {
		return
	}
	sum := sha256.Sum256([]byte(sessionToken))
	db.Exec(`DELETE FROM sessions WHERE token = ?`, hex.EncodeToString(sum[:]))
}

func newToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func normalize(email string) string {
	out := make([]rune, 0, len(email))
	for _, r := range email {
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		out = append(out, r)
	}
	return string(out)
}

func hasAt(s string) bool {
	for _, r := range s {
		if r == '@' {
			return true
		}
	}
	return false
}
