// Resend mailer — free tier (3,000 emails/month, 100/day), HTTP API (no SMTP
// handshake), best-in-class deliverability. Domain verification required
// (SPF/DKIM DNS records) for custom from-domain; onboarding sender works
// for testing. https://resend.com — RESEND_API_KEY in env.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const resendAPI = "https://api.resend.com/emails"

// ResendMailer — HTTP API mailer, free tier generous, one API key.
type ResendMailer struct {
	APIKey string
	From   string // "WatchLedger <hello@yourdomain>" — verified domain required
}

var _ Mailer = (*ResendMailer)(nil)

func (m *ResendMailer) send(to, subject, body string) error {
	if m.APIKey == "" {
		return fmt.Errorf("resend: RESEND_API_KEY not set")
	}
	from := m.From
	if from == "" {
		from = "WatchLedger <onboarding@resend.dev>"
	}
	payload, err := json.Marshal(map[string]any{
		"from":    from,
		"to":      []string{to},
		"subject": subject,
		"text":    body,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, resendAPI, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("resend: status %d", res.StatusCode)
	}
	return nil
}

func (m *ResendMailer) SendLoginLink(email, link string) error {
	escaped := url.PathEscape(link)
	return m.send(email, "Your WatchLedger sign-in link",
		fmt.Sprintf("Sign in to WatchLedger:\n\n%s\n\nThis link works once and expires in 15 minutes.\n"+
			"If you didn't request it, ignore this email — no account is created without it.\n\n"+
			"WatchLedger — every number re-derives from evidence.\n", escaped))
}

func (m *ResendMailer) SendAlert(email, subject, body string) error {
	return m.send(email, subject, body)
}
