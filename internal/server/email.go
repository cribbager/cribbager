package server

import (
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"
)

// Emailer sends a plaintext email. It is the seam between the server and a real
// mail transport: production wires an SMTPEmailer, while dev and tests use the
// LogEmailer (which logs instead of sending), so no mail server is required to
// exercise the password-reset flow.
type Emailer interface {
	Send(to, subject, body string) error
}

// LogEmailer logs the email — including the reset link in the body — instead of
// sending it. It is the default so local development works with no SMTP server
// configured: the reset link simply appears in the server log.
type LogEmailer struct{}

func (LogEmailer) Send(to, subject, body string) error {
	log.Printf("email (not sent; no SMTP configured) to=%s subject=%q\n%s", to, subject, body)
	return nil
}

// SMTPEmailer sends mail over SMTP via the stdlib net/smtp. smtp.SendMail
// negotiates STARTTLS automatically when the server advertises it. PlainAuth is
// used when a user is configured; otherwise auth is nil (e.g. Mailpit, which
// needs none).
type SMTPEmailer struct {
	host string // SMTP server host
	port string // SMTP server port
	user string // username for PLAIN auth ("" → no auth)
	pass string // password for PLAIN auth
	from string // envelope + From-header sender address
}

// NewSMTPEmailer builds an SMTPEmailer from explicit config. host and from are
// required by the caller; user may be empty to send without authentication.
func NewSMTPEmailer(host, port, user, pass, from string) *SMTPEmailer {
	return &SMTPEmailer{host: host, port: port, user: user, pass: pass, from: from}
}

func (e *SMTPEmailer) Send(to, subject, body string) error {
	var auth smtp.Auth
	if e.user != "" {
		auth = smtp.PlainAuth("", e.user, e.pass, e.host)
	}
	msg := buildMessage(e.from, to, subject, body)
	addr := e.host + ":" + e.port
	return smtp.SendMail(addr, auth, e.from, []string{to}, msg)
}

// buildMessage assembles a minimal RFC-5322 message: From/To/Subject/Date
// headers, a blank line, then the plaintext body. CRLF line endings per the
// spec. The body is left as-is (it carries only the reset link and short text).
func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	return []byte(b.String())
}
