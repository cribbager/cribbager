package server

import (
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ErrUserExists is the sentinel returned by AuthStore.CreateUser when the
// username or email is already taken. Callers map it to a 409 — and crucially
// the signup handler is the only place it surfaces, never login, so it can't be
// used to enumerate accounts.
var ErrUserExists = errors.New("user already exists")

// User is a registered account. PasswordHash is the bcrypt hash and must never
// be serialized to a client — handlers project a userResponse instead.
type User struct {
	ID           string
	Username     string
	Email        string
	DisplayName  string
	PasswordHash string
	CreatedAt    time.Time
}

// Session is a server-side login session. ID is the opaque, high-entropy value
// stored in the cribbager_session cookie; it is the only thing the client holds.
type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

// ResetToken is a single-use password-reset credential. ID is a high-entropy
// random token (newToken()) emailed to the user as part of the reset link; it is
// the only thing that proves the request and is deleted the moment a reset
// succeeds so a link can't be replayed.
type ResetToken struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

// AuthStore is the durable backing for accounts and login sessions. It mirrors
// the Store pattern: an interface with an in-memory implementation (MemAuthStore,
// for tests) and a Postgres implementation (PgAuthStore, for production).
//
// CreateUser enforces a unique Username AND Email and returns ErrUserExists on a
// conflict with either. Username/email lookups are case-insensitive.
type AuthStore interface {
	CreateUser(User) error
	UserByID(id string) (User, bool, error)
	UserByUsername(username string) (User, bool, error)
	UserByEmail(email string) (User, bool, error)
	UpdatePassword(userID, hash string) error

	CreateSession(Session) error
	SessionByID(id string) (Session, bool, error)
	DeleteSession(id string) error
	DeleteSessionsForUser(userID string) error // invalidate all of a user's logins (e.g. on password reset)

	CreateResetToken(ResetToken) error
	ResetTokenByID(id string) (ResetToken, bool, error)
	DeleteResetToken(id string) error

	// ReapExpired deletes sessions and reset tokens whose expiry has passed, so
	// those tables don't grow without bound. Called periodically.
	ReapExpired(now time.Time) error
}

// sessionCookieName is the cookie carrying the opaque session id.
const sessionCookieName = "cribbager_session"

// sessionTTL is how long a login session (and its cookie) lives.
const sessionTTL = 30 * 24 * time.Hour

// resetTokenTTL is how long a password-reset link is valid before it expires.
const resetTokenTTL = time.Hour

// hashPassword bcrypt-hashes a plaintext password at the default cost.
func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// checkPassword reports whether pw matches the bcrypt hash. CompareHashAndPassword
// is constant-time with respect to the password, so it leaks nothing via timing.
func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash is a valid bcrypt hash used solely to equalize login timing when no
// matching user exists: running a comparison against it makes a failed login cost
// the same whether or not the account exists, closing a timing-based enumeration
// channel (the plaintext is irrelevant). Computed once at startup.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("x"), bcrypt.DefaultCost)

// newUserID returns a fresh user id (128-bit random, URL-safe). It reuses the
// session token generator and truncates — a user id need not be as long as a
// secret session id, only collision-resistant.
func newUserID() string { return newToken()[:16] }

// setSessionCookie writes the session cookie. Secure is gated on s.secureCookies:
// it must be false for plain http://localhost (a Secure cookie is never sent over
// http, so login would silently fail) and true behind https in production.
func (s *Server) setSessionCookie(w http.ResponseWriter, sessionID string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie on the client (logout).
func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// currentUser resolves the logged-in user from the session cookie: read cookie →
// SessionByID → reject if missing or expired → UserByID. It returns ok=false for
// any miss (no cookie, unknown/expired session, vanished user), never an error to
// the caller — auth is best-effort and a failure simply means "not logged in".
func (s *Server) currentUser(r *http.Request) (User, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return User{}, false
	}
	sess, ok, err := s.auth.SessionByID(c.Value)
	if err != nil || !ok {
		return User{}, false
	}
	if !sess.ExpiresAt.After(s.now()) {
		return User{}, false
	}
	u, ok, err := s.auth.UserByID(sess.UserID)
	if err != nil || !ok {
		return User{}, false
	}
	return u, true
}
