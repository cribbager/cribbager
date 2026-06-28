package game

import (
	"errors"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// Command is a player's intent. Only two exist — everything else the engine runs
// automatically.
type Command interface{ isCommand() }

// Discard sheds two cards to the crib during the discard phase.
type Discard struct{ Cards [2]cribbage.Card }

// Play plays one card during the play (pegging) phase.
type Play struct{ Card cribbage.Card }

func (Discard) isCommand() {}
func (Play) isCommand()    {}

// Errors returned by Apply for illegal commands. No illegal command mutates
// state.
var (
	ErrGameOver         = errors.New("game: the game is over")
	ErrWrongPhase       = errors.New("game: command not valid in this phase")
	ErrNotYourTurn      = errors.New("game: not this seat's turn")
	ErrNotInHand        = errors.New("game: card is not in the seat's hand")
	ErrAlreadyDiscarded = errors.New("game: seat has already discarded")
	ErrDuplicateDiscard = errors.New("game: cannot discard the same card twice")
	ErrCountExceeds31   = errors.New("game: that card would take the count past 31")
	ErrUnknownCommand   = errors.New("game: unknown command")
)
