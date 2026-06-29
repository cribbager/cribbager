package server

import (
	"sync"
	"time"

	"github.com/cribbager/cribbager/internal/game"
)

// Record is the durable, serializable state of a session — everything needed to
// rebuild it after a process restart. Transient transport state (SSE subscribers,
// presence) is deliberately excluded: clients reconnect and it's rebuilt.
type Record struct {
	ID        string
	Game      game.GameState
	Tokens    [2]string
	Names     [2]string
	PlayerIDs [2]string // account id per seat ("" for a guest seat)
	Left      [2]bool
	Bots      [2]bool   // whether each seat is a bot (re-attach the champion on restore)
	Public    bool      // open game listable in the lobby (vs. private/link-only)
	CreatedAt time.Time // when the session was created (lobby "age")
	LastSeen  time.Time
}

// Store is the durable backing for sessions. The registry is the hot in-memory
// cache that serves the move/stream hot path; the Store is what survives a
// restart. Save is a full upsert (write-through on each change); LoadAll is read
// once at boot; Delete drops a finished/reaped game.
//
// Implementations: NoopStore (no durability — the default, current behavior),
// MemStore (in-process, for tests), and a Postgres store for production.
type Store interface {
	Save(Record) error
	LoadAll() ([]Record, error)
	Delete(id string) error
}

// NoopStore persists nothing: games live only in memory and are lost on restart.
// It's the default so the server runs with no database configured.
type NoopStore struct{}

func (NoopStore) Save(Record) error          { return nil }
func (NoopStore) LoadAll() ([]Record, error) { return nil, nil }
func (NoopStore) Delete(string) error        { return nil }

// MemStore is an in-process, map-backed Store. It exercises the full persistence
// path without a database, so tests can verify the save/restore round-trip.
type MemStore struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewMemStore() *MemStore { return &MemStore{records: map[string]Record{}} }

func (m *MemStore) Save(r Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[r.ID] = r
	return nil
}

func (m *MemStore) LoadAll() ([]Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, r)
	}
	return out, nil
}

func (m *MemStore) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, id)
	return nil
}
