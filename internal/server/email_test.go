package server

import (
	"strings"
	"testing"
)

// SMTP's MAIL FROM takes a bare address; the From: header takes the display form.
// Resend answers "501 Bad sender address syntax" if the display form reaches the
// envelope, so the two must be derived separately from one SMTP_FROM value.
func TestSMTPEmailerEnvelopeSender(t *testing.T) {
	cases := []struct {
		name string
		from string
		want string
	}{
		{"display name is stripped for the envelope",
			"Cribbager <noreply@cribbager.org>", "noreply@cribbager.org"},
		{"quoted display name is stripped too",
			`"Cribbager Support" <noreply@cribbager.org>`, "noreply@cribbager.org"},
		{"a bare address is used as-is",
			"noreply@cribbager.org", "noreply@cribbager.org"},
		{"angle brackets alone are unwrapped",
			"<noreply@cribbager.org>", "noreply@cribbager.org"},
		// Unparseable values are passed through rather than silently blanked: the
		// server reports its own error, which is easier to debug than an empty sender.
		{"an unparseable value falls back verbatim",
			"not an address", "not an address"},
		{"the main.go default is left alone", "cribbager@localhost", "cribbager@localhost"},
	}
	for _, c := range cases {
		e := NewSMTPEmailer("smtp.example.org", "587", "user", "pass", c.from)
		if e.envelope != c.want {
			t.Errorf("%s: envelope = %q, want %q", c.name, e.envelope, c.want)
		}
		if e.from != c.from {
			t.Errorf("%s: from = %q, want it unchanged as %q", c.name, e.from, c.from)
		}
	}
}

// The display name is the point of configuring one, so it must survive into the
// header even though the envelope drops it.
func TestBuildMessageKeepsDisplayName(t *testing.T) {
	from := "Cribbager <noreply@cribbager.org>"
	msg := string(buildMessage(from, "player@example.org", "Reset your password", "link"))
	if !strings.Contains(msg, "From: "+from+"\r\n") {
		t.Errorf("From header lost the display name:\n%s", msg)
	}
	if !strings.HasSuffix(msg, "\r\n\r\nlink") {
		t.Errorf("body not separated from headers by a blank line:\n%s", msg)
	}
}
