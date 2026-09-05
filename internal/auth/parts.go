package auth

import (
	"fmt"
	"strings"
)

// loginEmailBody — the shareable, clickable version.
// HTML with a real button + plain-text fallback (multipart for SMTP,
// both fields for Resend's API).
func loginEmailParts(link string) (subject, text, html string) {
	subject = "Your WatchLedger sign-in link"
	text = fmt.Sprintf(
		"Sign in to WatchLedger:\n\n%s\n\nThis link works once and expires in 15 minutes.\n"+
			"If you didn't request it, ignore this email — no account is created without it.\n\n"+
			"WatchLedger — every number re-derives from evidence.\n", link)
	html = fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;background:#f6f4ef;font-family:Georgia,serif;">
  <div style="max-width:480px;margin:0 auto;padding:32px 24px;background:#f6f4ef;color:#101418;">
    <div style="font-family:ui-monospace,Menlo,monospace;font-size:11px;letter-spacing:.18em;color:#a3762a;text-transform:uppercase;margin-bottom:16px;">WatchLedger</div>
    <h1 style="font-size:22px;margin:0 0 12px;letter-spacing:-0.02em;">Sign in to WatchLedger</h1>
    <p style="font-size:15px;line-height:1.6;margin:0 0 24px;">One click — that's the whole password policy.</p>
    <a href="%s" style="display:inline-block;background:#101418;color:#f6f4ef;padding:14px 28px;border-radius:8px;text-decoration:none;font-size:15px;font-weight:bold;">Sign in →</a>
    <p style="font-size:12px;color:#777;line-height:1.6;margin:24px 0 0;">This link works <b>once</b> and expires in 15 minutes.<br>
    If you didn't request it, ignore this email — no account is created without it.</p>
    <p style="font-size:11px;color:#999;margin-top:24px;">WatchLedger — every number re-derives from evidence.<br>
    <a href="%s" style="color:#a3762a;">watchfairvalue.com</a></p>
  </div>
</body></html>`, link, strings.TrimSuffix(link, "/"))
	return
}

func alertEmailParts(subject, body string) (string, string, string) {
	htmlBody := fmt.Sprintf(`<!doctype html>
<html><body style="margin:0;background:#f6f4ef;font-family:Georgia,serif;">
  <div style="max-width:480px;margin:0 auto;padding:32px 24px;background:#f6f4ef;color:#101418;">
    <div style="font-family:ui-monospace,Menlo,monospace;font-size:11px;letter-spacing:.18em;color:#a3762a;text-transform:uppercase;margin-bottom:16px;">WatchLedger</div>
    <h1 style="font-size:19px;margin:0 0 12px;">%s</h1>
    <div style="font-size:14px;line-height:1.6;white-space:pre-wrap;">%s</div>
    <p style="font-size:11px;color:#999;margin-top:24px;">You get this because you watch a reference. One-click removal on the watchlist.</p>
  </div>
</body></html>`, subject, body)
	return subject, body, htmlBody
}
