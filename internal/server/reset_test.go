package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeEmailer captures the last email sent so tests can assert a reset link was
// emailed (and dig the token out of it).
type fakeEmailer struct {
	mu   sync.Mutex
	sent []struct{ to, subject, body string }
}

func (f *fakeEmailer) Send(to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, struct{ to, subject, body string }{to, subject, body})
	return nil
}

func (f *fakeEmailer) last() (to, subject, body string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return "", "", "", false
	}
	e := f.sent[len(f.sent)-1]
	return e.to, e.subject, e.body, true
}

// waitFor blocks until at least n emails have been captured (or fails on timeout).
// The reset-request handler sends asynchronously, so tests must wait rather than
// read immediately after the request returns.
func (f *fakeEmailer) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		got := len(f.sent)
		f.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d email(s); got %d", n, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// resetClient is like authClient but exposes the underlying Server so tests can
// inject a fake emailer and control time.
type resetClient struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
	cli *http.Client
}

func newResetClient(t *testing.T) (*resetClient, *fakeEmailer) {
	t.Helper()
	srv := New()
	mail := &fakeEmailer{}
	srv.SetEmailer(mail)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &resetClient{t: t, srv: srv, ts: ts, cli: &http.Client{Jar: jar}}, mail
}

func (c *resetClient) do(method, path string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	resp, err := c.cli.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

// tokenFromLink pulls the ?token=<id> out of an emailed reset link.
func tokenFromLink(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "token=")
	if i < 0 {
		t.Fatalf("no token in email body: %q", body)
	}
	tok := body[i+len("token="):]
	if j := strings.IndexAny(tok, "\r\n \t"); j >= 0 {
		tok = tok[:j]
	}
	return tok
}

// TestResetRequestNoEnumeration asserts an existing and a nonexistent email both
// return 200 with the identical generic body, and that the existing one actually
// minted a token and emailed a link.
func TestResetRequestNoEnumeration(t *testing.T) {
	c, mail := newResetClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "gina", Email: "gina@example.com", Password: "password123",
	})
	c.do("POST", "/auth/logout", nil)

	respKnown, bodyKnown := c.do("POST", "/auth/password/reset-request", resetRequestRequest{Email: "gina@example.com"})
	respUnknown, bodyUnknown := c.do("POST", "/auth/password/reset-request", resetRequestRequest{Email: "nobody@example.com"})

	if respKnown.StatusCode != http.StatusOK {
		t.Errorf("known email: got %d, want 200", respKnown.StatusCode)
	}
	if respUnknown.StatusCode != http.StatusOK {
		t.Errorf("unknown email: got %d, want 200", respUnknown.StatusCode)
	}
	if !bytes.Equal(bodyKnown, bodyUnknown) {
		t.Errorf("enumeration: known body %q != unknown body %q", bodyKnown, bodyUnknown)
	}

	// The known email must have produced exactly one email with a reset link.
	mail.waitFor(t, 1) // the send is async
	to, _, body, ok := mail.last()
	if !ok {
		t.Fatal("no email sent for the known address")
	}
	if to != "gina@example.com" {
		t.Errorf("email to = %q, want gina@example.com", to)
	}
	if !strings.Contains(body, "/reset.html?token=") {
		t.Errorf("email body missing reset link: %q", body)
	}
	if len(mail.sent) != 1 {
		t.Errorf("sent %d emails, want 1 (none for the unknown address)", len(mail.sent))
	}
}

// TestResetChangesPassword walks the happy path: request → reset → the new
// password works, the old fails, and the token is single-use.
func TestResetChangesPassword(t *testing.T) {
	c, mail := newResetClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "hank", Email: "hank@example.com", Password: "password123",
	})
	c.do("POST", "/auth/logout", nil)

	c.do("POST", "/auth/password/reset-request", resetRequestRequest{Email: "hank@example.com"})
	mail.waitFor(t, 1) // the send is async
	_, _, body, ok := mail.last()
	if !ok {
		t.Fatal("no reset email")
	}
	token := tokenFromLink(t, body)

	// Reset to a new password.
	if resp, data := c.do("POST", "/auth/password/reset", resetRequest{Token: token, Password: "newpassword456"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset: got %d %s, want 204", resp.StatusCode, data)
	}

	// New password logs in; old password fails.
	if resp, _ := c.do("POST", "/auth/login", loginRequest{Username: "hank", Password: "newpassword456"}); resp.StatusCode != http.StatusOK {
		t.Errorf("login with new password: got %d, want 200", resp.StatusCode)
	}
	c.do("POST", "/auth/logout", nil)
	if resp, _ := c.do("POST", "/auth/login", loginRequest{Username: "hank", Password: "password123"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with old password: got %d, want 401", resp.StatusCode)
	}

	// Single-use: the same token can't reset again.
	if resp, _ := c.do("POST", "/auth/password/reset", resetRequest{Token: token, Password: "anotherpass789"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("reusing token: got %d, want 400", resp.StatusCode)
	}
}

// TestResetInvalidatesSessions: a password reset boots the user's existing
// logins, so a session held before the reset can no longer authenticate.
func TestResetInvalidatesSessions(t *testing.T) {
	c, mail := newResetClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "kate", Email: "kate@example.com", Password: "password123",
	})
	if resp, _ := c.do("GET", "/auth/me", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected to be logged in after signup, got %d", resp.StatusCode)
	}

	// Reset the password (the reset itself uses the emailed token, not the session).
	c.do("POST", "/auth/password/reset-request", resetRequestRequest{Email: "kate@example.com"})
	mail.waitFor(t, 1)
	_, _, body, _ := mail.last()
	token := tokenFromLink(t, body)
	if resp, _ := c.do("POST", "/auth/password/reset", resetRequest{Token: token, Password: "newpassword456"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset: got %d, want 204", resp.StatusCode)
	}

	// The pre-reset session must now be invalid.
	if resp, _ := c.do("GET", "/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session after reset: got %d, want 401 (should be invalidated)", resp.StatusCode)
	}
}

// TestResetTokenValidation covers expired, unknown, and short-password cases —
// all 400.
func TestResetTokenValidation(t *testing.T) {
	c, mail := newResetClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "iris", Email: "iris@example.com", Password: "password123",
	})

	// Unknown token → 400.
	if resp, _ := c.do("POST", "/auth/password/reset", resetRequest{Token: "does-not-exist", Password: "newpassword456"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown token: got %d, want 400", resp.StatusCode)
	}

	// Mint a real token, then short-password → 400 (token still valid, password bad).
	c.do("POST", "/auth/password/reset-request", resetRequestRequest{Email: "iris@example.com"})
	mail.waitFor(t, 1) // the send is async
	_, _, body, _ := mail.last()
	token := tokenFromLink(t, body)
	if resp, _ := c.do("POST", "/auth/password/reset", resetRequest{Token: token, Password: "short"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("short password: got %d, want 400", resp.StatusCode)
	}

	// Expire the token by advancing the server clock past its TTL → 400.
	c.srv.now = func() time.Time { return time.Now().Add(2 * resetTokenTTL) }
	if resp, _ := c.do("POST", "/auth/password/reset", resetRequest{Token: token, Password: "newpassword456"}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expired token: got %d, want 400", resp.StatusCode)
	}
}

// TestPgResetTokenRoundTrip is gated on TEST_DATABASE_URL. It exercises the
// password_resets table: create, lookup (hit + miss), and single-use delete.
func TestPgResetTokenRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the Postgres reset-token integration test")
	}
	pg, err := NewPgAuthStore(openTestDB(t, dsn))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM password_resets"); err != nil {
		t.Fatalf("clean password_resets: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM sessions"); err != nil {
		t.Fatalf("clean sessions: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM users"); err != nil {
		t.Fatalf("clean users: %v", err)
	}

	hash, _ := hashPassword("password123")
	u := User{ID: newUserID(), Username: "jane", Email: "jane@example.com", DisplayName: "Jane", PasswordHash: hash}
	if err := pg.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	tok := ResetToken{ID: newToken(), UserID: u.ID, ExpiresAt: time.Now().Add(resetTokenTTL).Round(time.Microsecond)}
	if err := pg.CreateResetToken(tok); err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	got, ok, err := pg.ResetTokenByID(tok.ID)
	if err != nil || !ok || got.UserID != u.ID {
		t.Fatalf("ResetTokenByID: %+v ok=%v err=%v", got, ok, err)
	}

	if err := pg.DeleteResetToken(tok.ID); err != nil {
		t.Fatalf("delete reset token: %v", err)
	}
	if _, ok, _ := pg.ResetTokenByID(tok.ID); ok {
		t.Error("reset token still present after delete")
	}
	if _, ok, _ := pg.ResetTokenByID("nope"); ok {
		t.Error("ResetTokenByID returned ok for unknown id")
	}
}
