package server

import (
	"strings"
	"sync"
	"time"
)

// MemAuthStore is an in-process, map-backed AuthStore. Like MemStore it exercises
// the full auth path without a database, so tests can verify signup/login/session
// flows. Username and email uniqueness are enforced case-insensitively.
type MemAuthStore struct {
	mu       sync.Mutex
	users    map[string]User       // by id
	sessions map[string]Session    // by id
	resets   map[string]ResetToken // by id
}

// NewMemAuthStore returns an empty in-memory auth store.
func NewMemAuthStore() *MemAuthStore {
	return &MemAuthStore{
		users:    map[string]User{},
		sessions: map[string]Session{},
		resets:   map[string]ResetToken{},
	}
}

func (m *MemAuthStore) CreateUser(u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.users {
		if strings.EqualFold(existing.Username, u.Username) || strings.EqualFold(existing.Email, u.Email) {
			return ErrUserExists
		}
	}
	m.users[u.ID] = u
	return nil
}

func (m *MemAuthStore) UserByID(id string) (User, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	return u, ok, nil
}

func (m *MemAuthStore) UserByUsername(username string) (User, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Username, username) {
			return u, true, nil
		}
	}
	return User{}, false, nil
}

func (m *MemAuthStore) UserByEmail(email string) (User, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if strings.EqualFold(u.Email, email) {
			return u, true, nil
		}
	}
	return User{}, false, nil
}

func (m *MemAuthStore) UpdatePassword(userID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return nil
	}
	u.PasswordHash = hash
	m.users[userID] = u
	return nil
}

func (m *MemAuthStore) CreateSession(s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *MemAuthStore) SessionByID(id string) (Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok, nil
}

func (m *MemAuthStore) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *MemAuthStore) DeleteSessionsForUser(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (m *MemAuthStore) CreateResetToken(t ResetToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resets[t.ID] = t
	return nil
}

func (m *MemAuthStore) ResetTokenByID(id string) (ResetToken, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.resets[id]
	return t, ok, nil
}

func (m *MemAuthStore) DeleteResetToken(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.resets, id)
	return nil
}

func (m *MemAuthStore) ReapExpired(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, id)
		}
	}
	for id, t := range m.resets {
		if !t.ExpiresAt.After(now) {
			delete(m.resets, id)
		}
	}
	return nil
}
