package game

import (
	"fmt"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// Decision reconstruction for post-game analysis. A finished game's event log is
// the fold source of ALL state a player decided from (View reads only foldable
// state — never the undealt deck remainder), so replaying the log through the
// engine's own reduce and capturing View at each play event reproduces exactly
// what the deciding seat saw. This lives in package game, not the server,
// because fidelity comes from reusing the unexported fold (reduce) and the real
// View — the same code paths the live game ran — rather than hand-assembling a
// look-alike view elsewhere.

// PlayDecision is one pegging choice a seat made in a finished game: the exact
// view the seat decided from and the card it actually played. It is the input a
// pegging analyzer needs to re-ask "what were the options, and what was chosen?".
type PlayDecision struct {
	Deal   int           // 0-based deal index within the game
	Seat   Seat          // who was on turn
	View   PlayerView    // exactly what View(Seat) returned at the moment of choice
	Played cribbage.Card // the card the seat actually played
}

// ReconstructPlays replays a finished game's event log and returns every pegging
// decision, in order. For each CardPlayed event it captures the deciding seat's
// View at the instant before the play — identical, field for field, to what the
// live game's View(seat) returned when the player chose (hand, pile, count,
// played cards, discards, starter, scores, legal plays, turn, version).
//
// Why folding is faithful: the live engine mutates state only in reduce (emit =
// reduce + append), and a play command emits its CardPlayed as the first event
// with no state change between validation and emission. So the folded state just
// before a CardPlayed event IS the resting state the player decided at, and
// View depends on nothing outside that folded state. In particular the undealt
// deck remainder (Game.rest, not recoverable from events) is never consulted:
// StarterCut and HandDealt events carry the cards that came off it.
//
// The log is validated at each decision point — right phase, right turn, card
// legal — so a corrupt or truncated log yields an error rather than a bogus
// decision. Passing a prefix of a live game's log (or a finished game's full
// log) both work; events after the last play are folded but produce nothing.
func ReconstructPlays(log []Event) ([]PlayDecision, error) {
	g := &Game{winner: -1}
	deal := -1
	var out []PlayDecision
	for i, e := range log {
		if p, ok := e.(CardPlayed); ok {
			if deal < 0 {
				return nil, fmt.Errorf("game: reconstruct: event %d: CardPlayed before any HandDealt", i)
			}
			v := g.View(p.Seat)
			if err := checkDecision(v, p); err != nil {
				return nil, fmt.Errorf("game: reconstruct: event %d: %w", i, err)
			}
			out = append(out, PlayDecision{Deal: deal, Seat: p.Seat, View: v, Played: p.Card})
		}
		if _, ok := e.(HandDealt); ok {
			deal++
		}
		// Mirror emit: reduce the event and count it, so View's Version matches
		// what the live game reported at the same point.
		g.reduce(e)
		g.version++
	}
	return out, nil
}

// checkDecision verifies that the folded state actually permitted the recorded
// play — the same conditions applyPlay validated live. Any violation means the
// log is not a legal game history.
func checkDecision(v PlayerView, p CardPlayed) error {
	if v.Phase != PhasePlay {
		return fmt.Errorf("CardPlayed by %v in phase %v", p.Seat, v.Phase)
	}
	if v.ToPlay == nil || *v.ToPlay != p.Seat {
		return fmt.Errorf("CardPlayed by %v out of turn", p.Seat)
	}
	if !contains(v.LegalPlays, p.Card) {
		return fmt.Errorf("CardPlayed %s by %v is not a legal play", p.Card, p.Seat)
	}
	return nil
}
