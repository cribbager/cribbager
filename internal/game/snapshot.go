package game

import "github.com/cribbager/cribbager/internal/cribbage"

// GameState is a serializable capture of a game's full state, for persistence.
// The event log is the source of all foldable state. Rest is the undealt deck
// remainder for the current hand — it determines the pending starter and is NOT
// derivable from the log, so it must be captured separately. Target is
// configuration, not an event.
type GameState struct {
	Log    []Event
	Rest   []cribbage.Card
	Target int
}

// Snapshot captures the game's state so it can be persisted and later Restored.
func (g *Game) Snapshot() GameState {
	return GameState{
		Log:    append([]Event(nil), g.log...),
		Rest:   append([]cribbage.Card(nil), g.rest...),
		Target: g.target,
	}
}

// Restore rebuilds a game from a Snapshot by folding the event log to reconstruct
// all foldable state — the same fold the engine's invariants are proven against
// (see foldEqual) — then attaches src for future deals. A fresh src is correct:
// every past shuffle is already captured in the log and Rest, and future hands
// are shuffled independently.
func Restore(s GameState, src DeckSource) *Game {
	g := &Game{target: s.Target, src: src, winner: -1}
	for _, e := range s.Log {
		g.reduce(e)
	}
	g.log = append([]Event(nil), s.Log...)
	g.version = len(s.Log)
	g.rest = append([]cribbage.Card(nil), s.Rest...)
	return g
}
