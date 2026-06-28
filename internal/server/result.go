package server

import (
	"sort"
	"sync"
	"time"

	"github.com/cribbager/cribbager/internal/game"
)

// Result is a completed game's permanent record. Written once on game-over and
// never reaped (unlike the live game in Store). PlayerIDs are "" for guest seats.
// Events is the full move log — kept for replay; the history list omits it.
type Result struct {
	ID        string
	PlayerIDs [2]string
	Names     [2]string
	Scores    [2]int
	Winner    int // winning seat (0 or 1)
	Events    []game.Event
	EndedAt   time.Time
}

// ResultStore is the durable record of finished games, keyed for per-player
// history. Like the other stores it has an in-memory implementation (tests) and a
// Postgres one (production).
type ResultStore interface {
	// SaveResult records a finished game. Idempotent: a duplicate id is ignored,
	// so replaying the game-over path can't double-write.
	SaveResult(Result) error
	// ResultsForPlayer returns a player's most recent games (newest first), up to
	// limit. Events is omitted (summaries only) to keep the list cheap.
	ResultsForPlayer(playerID string, limit int) ([]Result, error)
	// PlayerStats returns the player's total games and wins.
	PlayerStats(playerID string) (total, wins int, err error)
}

// MemResultStore is an in-process result store. It's the default (so local dev
// without a database still has working, if ephemeral, history) and backs the tests.
type MemResultStore struct {
	mu      sync.Mutex
	results map[string]Result
}

func NewMemResultStore() *MemResultStore { return &MemResultStore{results: map[string]Result{}} }

func (m *MemResultStore) SaveResult(r Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.results[r.ID]; exists {
		return nil // idempotent
	}
	m.results[r.ID] = r
	return nil
}

// involves reports whether playerID holds a (non-guest) seat in r.
func involves(r Result, playerID string) bool {
	return playerID != "" && (r.PlayerIDs[0] == playerID || r.PlayerIDs[1] == playerID)
}

func (m *MemResultStore) ResultsForPlayer(playerID string, limit int) ([]Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Result
	for _, r := range m.results {
		if involves(r, playerID) {
			r.Events = nil // summaries only
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndedAt.After(out[j].EndedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemResultStore) PlayerStats(playerID string) (total, wins int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.results {
		if !involves(r, playerID) {
			continue
		}
		total++
		if r.PlayerIDs[r.Winner] == playerID {
			wins++
		}
	}
	return total, wins, nil
}
