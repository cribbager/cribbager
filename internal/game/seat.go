// Package game is the cribbage rules engine: a pure, deterministic state
// machine for one 2-player game. It has no I/O, no notion of users or tokens,
// and makes no random calls itself — all entropy is injected via a DeckSource.
// The server layer (separate) owns active-game storage, authentication, and
// visibility filtering.
package game

import "fmt"

// Seat identifies one of the two players. Dealer and non-dealer ("pone") are
// roles derived from the Dealer field, not fixed to a seat.
type Seat uint8

const (
	Seat0 Seat = 0
	Seat1 Seat = 1
)

// other returns the opposing seat.
func other(s Seat) Seat { return s ^ 1 }

func (s Seat) String() string {
	if s == Seat0 {
		return "P0"
	}
	return "P1"
}

// Phase is the part of the hand awaiting a player decision. Cut and show are
// instantaneous engine transitions (represented by events), not resting phases.
type Phase uint8

const (
	PhaseDiscard Phase = iota
	PhasePlay
	PhaseComplete
)

func (p Phase) String() string {
	switch p {
	case PhaseDiscard:
		return "discard"
	case PhasePlay:
		return "play"
	case PhaseComplete:
		return "complete"
	default:
		return "?"
	}
}

// MarshalJSON encodes a phase as its name, so wire snapshots read "play" rather
// than a bare number.
func (p Phase) MarshalJSON() ([]byte, error) { return []byte(`"` + p.String() + `"`), nil }

// UnmarshalJSON decodes a phase from its name, so snapshots round-trip.
func (p *Phase) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case `"discard"`:
		*p = PhaseDiscard
	case `"play"`:
		*p = PhasePlay
	case `"complete"`:
		*p = PhaseComplete
	default:
		return fmt.Errorf("game: unknown phase %s", b)
	}
	return nil
}
