// Resend mailer — free tier (3,000 emails/month, 100/day), HTTP API, HTML + text.
package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

const resendAPI = "https://api.resend.com/emails"

// ResendMailer — HTTP API mailer, free tier generous, one API key.
type ResendMailer struct {
	APIKey string
	From   string
}

var _ Mailer = (*ResendMailer)(nil)

func (m *ResendMailer) send(to, subject, text, html string) error {
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
		"text":    text,
		"html":    html,
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
	subject, text, html := loginEmailParts(link)
	return m.send(email, subject, text, html)
}

func (m *ResendMailer) SendAlert(email, subject, body string) error {
	s, text, html := alertEmailParts(subject, body)
	return m.send(email, s, text, html)
}
