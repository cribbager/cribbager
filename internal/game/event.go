package game

import (
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// Event is one entry in a game's append-only log. The log is the source of
// truth: the live state is the fold of these events. Every state change is one
// event.
type Event interface{ isEvent() }

// CutForDeal records the opening cut that picks the first dealer (lower card
// deals). Cuts[seat] is that seat's cut card.
type CutForDeal struct {
	Cuts   [2]cribbage.Card
	Dealer Seat
}

// HandDealt starts a hand: the dealer and each seat's six cards.
type HandDealt struct {
	Dealer Seat
	Hands  [2][]cribbage.Card
}

// Discarded records a seat sending two cards to the crib.
type Discarded struct {
	Seat  Seat
	Cards [2]cribbage.Card
}

// StarterCut records the starter card and any his-heels points (0 or 2 to the
// dealer).
type StarterCut struct {
	Card  cribbage.Card
	Heels int
}

// CardPlayed records a play during pegging and the points it scored.
type CardPlayed struct {
	Seat  Seat
	Card  cribbage.Card
	Score pegging.Result
}

// Pass records that the seat on turn could not legally play (a "go").
type Pass struct{ Seat Seat }

// GoAwarded records the 1 point for the go / last card to the last player who
// could play in a series.
type GoAwarded struct {
	Seat   Seat
	Points int
}

// SeriesReset records the count resetting to zero (after 31 or a go); Leader is
// the seat that leads the next series.
type SeriesReset struct{ Leader Seat }

// HandShown records a player's hand count at the show.
type HandShown struct {
	Seat  Seat
	Cards []cribbage.Card
	Score hand.Result
}

// CribShown records the dealer's crib count at the show.
type CribShown struct {
	Cards []cribbage.Card
	Score hand.Result
}

// GameWon records that a seat reached the target score.
type GameWon struct{ Seat Seat }

func (CutForDeal) isEvent()  {}
func (HandDealt) isEvent()   {}
func (Discarded) isEvent()   {}
func (StarterCut) isEvent()  {}
func (CardPlayed) isEvent()  {}
func (Pass) isEvent()        {}
func (GoAwarded) isEvent()   {}
func (SeriesReset) isEvent() {}
func (HandShown) isEvent()   {}
func (CribShown) isEvent()   {}
func (GameWon) isEvent()     {}
