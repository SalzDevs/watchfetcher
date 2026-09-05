package auth

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"net/url"
	"strings"
)

// SMTPMailer — real email delivery via any SMTP provider (Postmark, Mailgun,
// SES, Gmail app-password). STARTTLS required; port 587 typical.
type SMTPMailer struct {
	Host     string // smtp.postmarkapp.com / smtp.gmail.com / email-smtp.eu-west-1.amazonaws.com
	Port     string // "587"
	User     string
	Password string
	From     string // "WatchLedger <hello@watchfairvalue.com>"
}

var _ Mailer = (*SMTPMailer)(nil)

func (m *SMTPMailer) send(to, subject, body string) error {
	if m.Host == "" || m.From == "" {
		return fmt.Errorf("smtp: host/from not configured")
	}
	addr := m.Host + ":" + m.Port
	msg := buildMessage(m.From, to, subject, body)

	host := m.Host
	var auth smtp.Auth
	if m.User != "" {
		host = hostnameOnly(m.Host)
		auth = smtp.PlainAuth("", m.User, m.Password, host)
	}

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		// fallback: STARTTLS on 587
		return smtp.SendMail(addr, auth, m.From, []string{to}, msg)
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}
	if err := c.Mail(m.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (m *SMTPMailer) fromAddress() string {
	// "WatchLedger <hello@x.com>" → bare address for envelope; keep pretty header
	if i := strings.Index(m.From, "<"); i >= 0 && strings.HasSuffix(m.From, ">") {
		return strings.TrimSuffix(m.From[i+1:], ">")
	}
	return m.From
}

func (m *SMTPMailer) SendLoginLink(email, link string) error {
	escaped := url.PathEscape(link)
	body := fmt.Sprintf(
		"Sign in to WatchLedger:\n\n%s\n\nThis link works once and expires in 15 minutes.\n"+
			"If you didn't request it, ignore this email — no account is created without it.\n\n"+
			"WatchLedger — every number re-derives from evidence.\n", escaped)
	return m.send(email, "Your WatchLedger sign-in link", body)
}

func (m *SMTPMailer) SendAlert(email, subject, body string) error {
	return m.send(email, subject, body)
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

func hostnameOnly(host string) string {
	if i := strings.Index(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}
