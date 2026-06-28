package server

import (
	"log"
	"net/http"
	"net/url"
	"strings"
)

// userResponse is the client-facing projection of a User. It deliberately omits
// PasswordHash (and CreatedAt) so the hash can never be serialized to a client.
type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

func toUserResponse(u User) userResponse {
	return userResponse{ID: u.ID, Username: u.Username, Email: u.Email, DisplayName: u.DisplayName}
}

type signupRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

// loginRequest accepts either a username or an email in the Username field; the
// handler looks up by username first, then email.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// validUsername enforces 3–24 chars of letters, digits, '_' or '-'. Username and
// email are trimmed and lowercased by the caller before this check.
func validUsername(u string) bool {
	if len(u) < 3 || len(u) > 24 {
		return false
	}
	for _, r := range u {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// validEmail is a deliberately loose check: it requires an '@' with a '.' somewhere
// after it, and rejects whitespace/control characters (which would otherwise let a
// stored address carry a CRLF — net/smtp already blocks send-time injection, but
// this keeps such values out of the database in the first place). Real
// deliverability is verified out of band, not here.
func validEmail(e string) bool {
	if strings.ContainsAny(e, " \t\r\n") {
		return false
	}
	at := strings.IndexByte(e, '@')
	if at <= 0 || at == len(e)-1 {
		return false
	}
	domain := e[at+1:]
	// Require a dot that is neither the first nor last char of the domain, so a
	// non-empty label sits on each side (rejects "a@b." and "a@.com").
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// sameOriginOK is belt-and-suspenders CSRF hardening for the mutating cookie
// endpoints: if an Origin header is present, its host must match the request
// Host. SameSite=Lax is the primary defense; this rejects cross-site form posts
// that slip past it. A request with no Origin header (e.g. server-to-server, or a
// same-origin GET-turned-POST) is allowed — the cookie's SameSite still governs.
func sameOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return host == r.Host
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	if !s.authLimiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many requests; please wait and try again")
		return
	}
	var req signupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !validUsername(username) {
		writeErr(w, http.StatusBadRequest, "username must be 3-24 chars of letters, digits, _ or -")
		return
	}
	if !validEmail(email) {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	display := strings.TrimSpace(req.DisplayName)
	if display == "" {
		display = username
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create account")
		return
	}
	u := User{
		ID:           newUserID(),
		Username:     username,
		Email:        email,
		DisplayName:  display,
		PasswordHash: hash,
		CreatedAt:    s.now(),
	}
	if err := s.auth.CreateUser(u); err != nil {
		if err == ErrUserExists {
			writeErr(w, http.StatusConflict, "username or email already taken")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not create account")
		return
	}
	if !s.startSession(w, u.ID) {
		writeErr(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusCreated, toUserResponse(u))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	if !s.loginLimiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts; please wait and try again")
		return
	}
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ident := strings.ToLower(strings.TrimSpace(req.Username))

	// Look up by username, then email. Every failure path below returns the same
	// generic 401 so the response never reveals whether the account exists or
	// whether it was the password that was wrong (no user enumeration).
	u, ok, err := s.auth.UserByUsername(ident)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	if !ok {
		u, ok, err = s.auth.UserByEmail(ident)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "login failed")
			return
		}
	}
	if !ok {
		// No such user: still run a comparison against a dummy hash so the response
		// time doesn't reveal whether the account exists (timing-enumeration defense).
		checkPassword(string(dummyHash), req.Password)
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !checkPassword(u.PasswordHash, req.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !s.startSession(w, u.ID) {
		writeErr(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = s.auth.DeleteSession(c.Value)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(u))
}

type resetRequestRequest struct {
	Email string `json:"email"`
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// resetGenericMessage is the single, fixed response to a reset-request. It is
// returned whether or not the email matches an account, so the endpoint never
// reveals which addresses are registered (anti-enumeration).
const resetGenericMessage = "if that email exists, a reset link was sent"

// handlePasswordResetRequest starts a reset flow: look up the user by email and,
// if found, mint a single-use token and email a reset link. The response is
// always 200 with the same generic body regardless of whether the account
// exists, so an attacker can't enumerate registered emails through this endpoint.
func (s *Server) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	if !s.authLimiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many requests; please wait and try again")
		return
	}
	var req resetRequestRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// Look the user up; on any miss (unknown email or store error) we still return
	// the same generic 200 below — only the side effect (mint + email) differs.
	if u, ok, err := s.auth.UserByEmail(email); err == nil && ok {
		tok := ResetToken{ID: newToken(), UserID: u.ID, ExpiresAt: s.now().Add(resetTokenTTL)}
		if err := s.auth.CreateResetToken(tok); err == nil {
			link := s.resetLink(r, tok.ID)
			body := "You (or someone) requested a password reset for your Cribbager account.\n\n" +
				"Open this link to choose a new password (it expires in 1 hour):\n\n" +
				link + "\n\n" +
				"If you didn't request this, you can ignore this email."
			to := u.Email
			// Send asynchronously: don't block the HTTP response on (potentially slow)
			// SMTP, and don't let send latency reveal whether the account exists.
			go func() {
				if err := s.emailer.Send(to, "Reset your Cribbager password", body); err != nil {
					log.Printf("password reset: send email to %s: %v", to, err)
				}
			}()
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": resetGenericMessage})
}

// handlePasswordReset consumes a reset token and sets a new password. The token
// must exist and be unexpired; on success the password is updated and the token
// is deleted (single-use) so the link can't be replayed.
func (s *Server) handlePasswordReset(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeErr(w, http.StatusForbidden, "cross-origin request rejected")
		return
	}
	// Rate-limit token consumption too: this is the one endpoint where guessing a
	// secret directly grants account takeover, so it must be bounded like the rest.
	if !s.authLimiter.allow(clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "too many requests; please wait and try again")
		return
	}
	var req resetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tok, ok, err := s.auth.ResetTokenByID(strings.TrimSpace(req.Token))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	if !ok || !tok.ExpiresAt.After(s.now()) {
		writeErr(w, http.StatusBadRequest, "invalid or expired token")
		return
	}
	if len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	if err := s.auth.UpdatePassword(tok.UserID, hash); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not reset password")
		return
	}
	// Single-use: delete the token so the same link can't reset twice.
	_ = s.auth.DeleteResetToken(tok.ID)
	// Invalidate every existing login for this user: a reset (often "I lost
	// access") should boot any other/stale sessions, including an attacker's.
	_ = s.auth.DeleteSessionsForUser(tok.UserID)
	w.WriteHeader(http.StatusNoContent)
}

// resetLink builds the absolute reset URL the user clicks. It prefers the pinned
// baseURL (BASE_URL env); otherwise it reconstructs scheme://host from the
// request, honoring X-Forwarded-Proto so links are https behind a TLS proxy.
func (s *Server) resetLink(r *http.Request, token string) string {
	base := s.baseURL
	if base == "" {
		scheme := "http"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	return base + "/reset.html?token=" + url.QueryEscape(token)
}

// startSession creates a session row with the standard TTL and sets the cookie.
// It returns false if the store rejects the session create.
func (s *Server) startSession(w http.ResponseWriter, userID string) bool {
	expires := s.now().Add(sessionTTL)
	sess := Session{ID: newToken(), UserID: userID, ExpiresAt: expires}
	if err := s.auth.CreateSession(sess); err != nil {
		return false
	}
	s.setSessionCookie(w, sess.ID, expires)
	return true
}
