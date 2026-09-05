package auth

import (
	"crypto/sha1"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// SMTPMailer — real email delivery via any SMTP provider (Postmark, Mailgun,
// SES, Gmail app-password). STARTTLS required; port 587 typical.
type SMTPMailer struct {
	Host     string
	Port     string // "587"
	User     string
	Password string
	From     string
}

var _ Mailer = (*SMTPMailer)(nil)

func (m *SMTPMailer) sendMultipart(to, subject, text, html string) error {
	if m.Host == "" || m.From == "" {
		return fmt.Errorf("smtp: host/from not configured")
	}
	addr := m.Host + ":" + m.Port
	host := hostnameOnly(m.Host)
	var auth smtp.Auth
	if m.User != "" {
		auth = smtp.PlainAuth("", m.User, m.Password, host)
	}

	boundary := "wl" + sha1Sum(subject+to)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n"+
		"Content-Type: multipart/alternative; boundary=%s\r\n\r\n"+
		"--%s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n"+
		"--%s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n"+
		"--%s--\r\n",
		m.From, to, subject, boundary, boundary, text, boundary, html, boundary)

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		// fallback: STARTTLS on 587
		return smtp.SendMail(addr, auth, m.From, []string{to}, []byte(msg))
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
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (m *SMTPMailer) SendLoginLink(email, link string) error {
	subject, text, html := loginEmailParts(link)
	return m.sendMultipart(email, subject, text, html)
}

func (m *SMTPMailer) SendAlert(email, subject, body string) error {
	s, text, html := alertEmailParts(subject, body)
	return m.sendMultipart(email, s, text, html)
}

func sha1Sum(s string) string {
	sum := sha1.Sum([]byte(s))
	return strings.ToUpper(fmt.Sprintf("%x", sum))[:24]
}

func hostnameOnly(host string) string {
	if i := strings.Index(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}
