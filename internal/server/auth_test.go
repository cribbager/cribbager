package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// authClient drives the /auth API with a cookie jar, so the session cookie set by
// signup/login flows through to subsequent requests (like a browser).
type authClient struct {
	t   *testing.T
	ts  *httptest.Server
	cli *http.Client
}

func newAuthClient(t *testing.T) *authClient {
	t.Helper()
	ts := httptest.NewServer(New().Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &authClient{t: t, ts: ts, cli: &http.Client{Jar: jar}}
}

func (c *authClient) do(method, path string, body any) (*http.Response, []byte) {
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

// hasSessionCookie reports whether the response set the session cookie to a
// non-empty value (i.e. a session was started).
func hasSessionCookie(resp *http.Response) bool {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestSignupSetsCookieAndMe(t *testing.T) {
	c := newAuthClient(t)
	resp, data := c.do("POST", "/auth/signup", signupRequest{
		Username: "Alice", Email: "alice@example.com", Password: "password123",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("signup: %d %s", resp.StatusCode, data)
	}
	if !hasSessionCookie(resp) {
		t.Fatal("signup did not set a session cookie")
	}
	u := decode[userResponse](t, data)
	if u.Username != "alice" { // lowercased
		t.Errorf("username = %q, want alice", u.Username)
	}
	if u.DisplayName != "alice" { // defaulted to username
		t.Errorf("display_name = %q, want alice", u.DisplayName)
	}
	// The password hash must never appear in the response body.
	if bytes.Contains(data, []byte("password_hash")) || bytes.Contains(data, []byte("PasswordHash")) {
		t.Errorf("signup leaked password hash: %s", data)
	}

	// /auth/me returns the logged-in user (cookie flows via the jar).
	resp, data = c.do("GET", "/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me after signup: %d %s", resp.StatusCode, data)
	}
	if decode[userResponse](t, data).Username != "alice" {
		t.Errorf("me returned wrong user: %s", data)
	}

	// Logout clears the session; /auth/me is then 401.
	resp, _ = c.do("POST", "/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", "/auth/me", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("me after logout: got %d, want 401", resp.StatusCode)
	}
}

func TestLoginByUsernameAndEmail(t *testing.T) {
	c := newAuthClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "bob", Email: "bob@example.com", Password: "password123",
	})
	c.do("POST", "/auth/logout", nil)

	// Login by username.
	if resp, data := c.do("POST", "/auth/login", loginRequest{Username: "bob", Password: "password123"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("login by username: %d %s", resp.StatusCode, data)
	}
	c.do("POST", "/auth/logout", nil)

	// Login by email (in the username field), case-insensitively.
	if resp, data := c.do("POST", "/auth/login", loginRequest{Username: "BOB@example.com", Password: "password123"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("login by email: %d %s", resp.StatusCode, data)
	}
}

// TestLoginNoUserEnumeration asserts that a wrong password and an unknown user
// return an identical 401 body, so a caller can't tell which accounts exist.
func TestLoginNoUserEnumeration(t *testing.T) {
	c := newAuthClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "carol", Email: "carol@example.com", Password: "password123",
	})
	c.do("POST", "/auth/logout", nil)

	respWrong, bodyWrong := c.do("POST", "/auth/login", loginRequest{Username: "carol", Password: "wrongpassword"})
	respUnknown, bodyUnknown := c.do("POST", "/auth/login", loginRequest{Username: "nobody", Password: "wrongpassword"})

	if respWrong.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: got %d, want 401", respWrong.StatusCode)
	}
	if respUnknown.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown user: got %d, want 401", respUnknown.StatusCode)
	}
	if !bytes.Equal(bodyWrong, bodyUnknown) {
		t.Errorf("user enumeration: wrong-password body %q != unknown-user body %q", bodyWrong, bodyUnknown)
	}
	// Neither failure may set a session cookie.
	if hasSessionCookie(respWrong) || hasSessionCookie(respUnknown) {
		t.Error("failed login set a session cookie")
	}
}

func TestSignupDuplicateConflict(t *testing.T) {
	c := newAuthClient(t)
	c.do("POST", "/auth/signup", signupRequest{
		Username: "dave", Email: "dave@example.com", Password: "password123",
	})

	// Duplicate username (different email) → 409.
	if resp, _ := c.do("POST", "/auth/signup", signupRequest{
		Username: "dave", Email: "other@example.com", Password: "password123",
	}); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate username: got %d, want 409", resp.StatusCode)
	}
	// Duplicate email (different username) → 409.
	if resp, _ := c.do("POST", "/auth/signup", signupRequest{
		Username: "dave2", Email: "dave@example.com", Password: "password123",
	}); resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate email: got %d, want 409", resp.StatusCode)
	}
	// Case-insensitive duplicate username → 409.
	if resp, _ := c.do("POST", "/auth/signup", signupRequest{
		Username: "DAVE", Email: "fresh@example.com", Password: "password123",
	}); resp.StatusCode != http.StatusConflict {
		t.Errorf("case-insensitive duplicate username: got %d, want 409", resp.StatusCode)
	}
}

func TestSignupValidation(t *testing.T) {
	c := newAuthClient(t)
	cases := []struct {
		name string
		req  signupRequest
	}{
		{"short password", signupRequest{Username: "eve", Email: "eve@example.com", Password: "short"}},
		{"bad email", signupRequest{Username: "eve", Email: "not-an-email", Password: "password123"}},
		{"short username", signupRequest{Username: "ab", Email: "eve@example.com", Password: "password123"}},
		{"bad username chars", signupRequest{Username: "eve!!", Email: "eve@example.com", Password: "password123"}},
	}
	for _, tc := range cases {
		if resp, data := c.do("POST", "/auth/signup", tc.req); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400 (%s)", tc.name, resp.StatusCode, data)
		}
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("hunter2sosecure")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "hunter2sosecure" {
		t.Fatal("password stored in plaintext")
	}
	if !checkPassword(hash, "hunter2sosecure") {
		t.Error("correct password did not verify")
	}
	if checkPassword(hash, "wrongpassword") {
		t.Error("wrong password verified")
	}
	// A tampered hash must fail (and not panic). Flip a byte in the digest portion
	// (bcrypt ignores trailing bytes, so appending wouldn't change the result).
	tampered := []byte(hash)
	if tampered[len(tampered)-1] == 'a' {
		tampered[len(tampered)-1] = 'b'
	} else {
		tampered[len(tampered)-1] = 'a'
	}
	if checkPassword(string(tampered), "hunter2sosecure") {
		t.Error("tampered hash verified")
	}
}

// TestPgAuthStoreRoundTrip is gated on TEST_DATABASE_URL, mirroring
// TestPgStoreRoundTrip. It exercises the real CreateUser (and dup → ErrUserExists),
// session create/get/delete, and UpdatePassword against Postgres.
func TestPgAuthStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the Postgres auth integration test")
	}
	pg, err := NewPgAuthStore(openTestDB(t, dsn))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM sessions"); err != nil {
		t.Fatalf("clean sessions: %v", err)
	}
	if _, err := pg.db.Exec("DELETE FROM users"); err != nil {
		t.Fatalf("clean users: %v", err)
	}

	hash, _ := hashPassword("password123")
	u := User{
		ID: newUserID(), Username: "frank", Email: "frank@example.com",
		DisplayName: "Frank", PasswordHash: hash,
	}
	if err := pg.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Duplicate username and email both map to ErrUserExists.
	if err := pg.CreateUser(User{ID: newUserID(), Username: "frank", Email: "x@y.com", PasswordHash: hash}); err != ErrUserExists {
		t.Errorf("dup username: got %v, want ErrUserExists", err)
	}
	if err := pg.CreateUser(User{ID: newUserID(), Username: "other", Email: "frank@example.com", PasswordHash: hash}); err != ErrUserExists {
		t.Errorf("dup email: got %v, want ErrUserExists", err)
	}

	// Case-insensitive lookups by username and email.
	if got, ok, err := pg.UserByUsername("FRANK"); err != nil || !ok || got.ID != u.ID {
		t.Errorf("UserByUsername(FRANK): %+v ok=%v err=%v", got, ok, err)
	}
	if got, ok, err := pg.UserByEmail("Frank@Example.com"); err != nil || !ok || got.ID != u.ID {
		t.Errorf("UserByEmail: %+v ok=%v err=%v", got, ok, err)
	}
	if _, ok, _ := pg.UserByID("does-not-exist"); ok {
		t.Error("UserByID returned ok for missing id")
	}

	// UpdatePassword changes the stored hash.
	newHash, _ := hashPassword("newpassword456")
	if err := pg.UpdatePassword(u.ID, newHash); err != nil {
		t.Fatalf("update password: %v", err)
	}
	if got, _, _ := pg.UserByID(u.ID); got.PasswordHash != newHash {
		t.Error("UpdatePassword did not persist the new hash")
	}

	// Session create / get / delete.
	sess := Session{ID: newToken(), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	if err := pg.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if got, ok, err := pg.SessionByID(sess.ID); err != nil || !ok || got.UserID != u.ID {
		t.Errorf("SessionByID: %+v ok=%v err=%v", got, ok, err)
	}
	if err := pg.DeleteSession(sess.ID); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, ok, _ := pg.SessionByID(sess.ID); ok {
		t.Error("session still present after delete")
	}
}
