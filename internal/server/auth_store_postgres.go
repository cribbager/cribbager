package server

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq" // also registers the database/sql "postgres" driver
)

// PgAuthStore is a Postgres-backed AuthStore using the shared pool (OpenPg).
// Username and email are stored lowercased so the UNIQUE constraints and the
// lookups are case-insensitive without needing citext.
type PgAuthStore struct {
	db *sql.DB
}

// NewPgAuthStore ensures the users, sessions, and password_resets tables exist on
// the shared pool and returns the store.
func NewPgAuthStore(db *sql.DB) (*PgAuthStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id            TEXT PRIMARY KEY,
			username      TEXT UNIQUE NOT NULL,
			email         TEXT UNIQUE NOT NULL,
			display_name  TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS password_resets (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, err
		}
	}
	return &PgAuthStore{db: db}, nil
}

func (p *PgAuthStore) CreateUser(u User) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, display_name, password_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		u.ID, strings.ToLower(u.Username), strings.ToLower(u.Email),
		u.DisplayName, u.PasswordHash, u.CreatedAt)
	if isUniqueViolation(err) {
		return ErrUserExists
	}
	return err
}

func (p *PgAuthStore) UserByID(id string) (User, bool, error) {
	return p.queryUser(`SELECT id, username, email, display_name, password_hash, created_at
		FROM users WHERE id = $1`, id)
}

func (p *PgAuthStore) UserByUsername(username string) (User, bool, error) {
	return p.queryUser(`SELECT id, username, email, display_name, password_hash, created_at
		FROM users WHERE username = $1`, strings.ToLower(username))
}

func (p *PgAuthStore) UserByEmail(email string) (User, bool, error) {
	return p.queryUser(`SELECT id, username, email, display_name, password_hash, created_at
		FROM users WHERE email = $1`, strings.ToLower(email))
}

// queryUser runs a single-row user lookup, returning ok=false (not an error) when
// no row matches.
func (p *PgAuthStore) queryUser(q string, arg string) (User, bool, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	var u User
	err := p.db.QueryRowContext(ctx, q, arg).Scan(
		&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return u, true, nil
}

func (p *PgAuthStore) UpdatePassword(userID, hash string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, userID)
	return err
}

func (p *PgAuthStore) CreateSession(s Session) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		s.ID, s.UserID, s.ExpiresAt)
	return err
}

func (p *PgAuthStore) SessionByID(id string) (Session, bool, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	var s Session
	var expires time.Time
	err := p.db.QueryRowContext(ctx, `SELECT id, user_id, expires_at FROM sessions WHERE id = $1`, id).
		Scan(&s.ID, &s.UserID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	s.ExpiresAt = expires
	return s, true, nil
}

func (p *PgAuthStore) DeleteSession(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (p *PgAuthStore) DeleteSessionsForUser(userID string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (p *PgAuthStore) CreateResetToken(t ResetToken) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `INSERT INTO password_resets (id, user_id, expires_at) VALUES ($1, $2, $3)`,
		t.ID, t.UserID, t.ExpiresAt)
	return err
}

func (p *PgAuthStore) ResetTokenByID(id string) (ResetToken, bool, error) {
	ctx, cancel := dbCtx()
	defer cancel()
	var t ResetToken
	var expires time.Time
	err := p.db.QueryRowContext(ctx, `SELECT id, user_id, expires_at FROM password_resets WHERE id = $1`, id).
		Scan(&t.ID, &t.UserID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ResetToken{}, false, nil
	}
	if err != nil {
		return ResetToken{}, false, err
	}
	t.ExpiresAt = expires
	return t, true, nil
}

func (p *PgAuthStore) DeleteResetToken(id string) error {
	ctx, cancel := dbCtx()
	defer cancel()
	_, err := p.db.ExecContext(ctx, `DELETE FROM password_resets WHERE id = $1`, id)
	return err
}

func (p *PgAuthStore) ReapExpired(now time.Time) error {
	ctx, cancel := dbCtx()
	defer cancel()
	if _, err := p.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < $1`, now); err != nil {
		return err
	}
	_, err := p.db.ExecContext(ctx, `DELETE FROM password_resets WHERE expires_at < $1`, now)
	return err
}

// isUniqueViolation reports whether err is a Postgres unique-constraint violation
// (SQLSTATE 23505), which is how a duplicate username or email surfaces on insert.
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
