package game

import (
	"fmt"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

const defaultTarget = 121

// Options configure a new game.
type Options struct {
	Deck        DeckSource // required: source of shuffled decks
	TargetScore int        // default 121
}

// Game is a single cribbage game. It is not safe for concurrent use; the caller
// (server) must serialize commands per game.
type Game struct {
	target int
	src    DeckSource
	log    []Event

	// foldable state (rebuilt by reduce):
	dealer     Seat
	scores     [2]int
	phase      Phase
	hands      [2][]cribbage.Card
	played     [2][]cribbage.Card
	crib       []cribbage.Card
	starter    cribbage.Card
	hasStarter bool
	discarded  [2]bool
	pile       []cribbage.Card
	count      int
	gone       [2]bool
	lastPlayer Seat
	winner     int // -1 until won

	// transient (live only, not part of the fold):
	rest    []cribbage.Card // remaining undealt deck for the current hand
	version int
}

// New starts a game: it cuts for the deal and deals the first hand, leaving the
// game in the discard phase.
func New(opts Options) *Game {
	target := opts.TargetScore
	if target <= 0 {
		target = defaultTarget
	}
	g := &Game{target: target, src: opts.Deck, winner: -1}

	cuts, dealer := cutForDeal(g.src)
	g.emit(CutForDeal{Cuts: cuts, Dealer: dealer})
	g.dealHand(dealer)
	return g
}

// cutForDeal shuffles and compares each player's cut; the lower rank deals. Ties
// re-cut.
func cutForDeal(src DeckSource) ([2]cribbage.Card, Seat) {
	for {
		d := src.Shuffle()
		a, b := d[0], d[1]
		if a.Rank == b.Rank {
			continue
		}
		dealer := Seat0
		if a.Rank > b.Rank {
			dealer = Seat1
		}
		return [2]cribbage.Card{a, b}, dealer
	}
}

// dealHand shuffles a fresh deck and deals six cards each, non-dealer first,
// leaving the rest as the undealt deck.
func (g *Game) dealHand(dealer Seat) {
	d := g.src.Shuffle()
	pone := other(dealer)
	var hands [2][]cribbage.Card
	for i := 0; i < 6; i++ {
		hands[pone] = append(hands[pone], d[2*i])
		hands[dealer] = append(hands[dealer], d[2*i+1])
	}
	g.rest = append([]cribbage.Card(nil), d[12:]...)
	g.emit(HandDealt{Dealer: dealer, Hands: hands})
}

// --- command handling ---------------------------------------------------------

// Apply validates and applies a command, returning the events it produced.
func (g *Game) Apply(seat Seat, cmd Command) ([]Event, error) {
	if g.winner >= 0 {
		return nil, ErrGameOver
	}
	start := len(g.log)
	var err error
	switch c := cmd.(type) {
	case Discard:
		err = g.applyDiscard(seat, c)
	case Play:
		err = g.applyPlay(seat, c)
	default:
		err = ErrUnknownCommand
	}
	if err != nil {
		// No partial events are emitted before validation passes, so the log is
		// unchanged on error.
		return nil, err
	}
	return append([]Event(nil), g.log[start:]...), nil
}

func (g *Game) applyDiscard(seat Seat, c Discard) error {
	if g.phase != PhaseDiscard {
		return ErrWrongPhase
	}
	if g.discarded[seat] {
		return ErrAlreadyDiscarded
	}
	if c.Cards[0] == c.Cards[1] {
		return ErrDuplicateDiscard
	}
	for _, card := range c.Cards {
		if !contains(g.hands[seat], card) {
			return fmt.Errorf("%w: %s", ErrNotInHand, card)
		}
	}

	g.emit(Discarded{Seat: seat, Cards: c.Cards})
	if g.discarded[Seat0] && g.discarded[Seat1] {
		g.cutStarter()
	}
	return nil
}

func (g *Game) cutStarter() {
	starter := g.rest[0]
	g.rest = g.rest[1:]
	heels := 0
	if starter.Rank == cribbage.Jack {
		heels = 2
	}
	g.emit(StarterCut{Card: starter, Heels: heels})
	g.checkWin(g.dealer)
}

func (g *Game) applyPlay(seat Seat, c Play) error {
	if g.phase != PhasePlay {
		return ErrWrongPhase
	}
	if seat != g.turn() {
		return ErrNotYourTurn
	}
	if !contains(g.hands[seat], c.Card) {
		return fmt.Errorf("%w: %s", ErrNotInHand, c.Card)
	}
	if !g.playable(c.Card) {
		return fmt.Errorf("%w: %s", ErrCountExceeds31, c.Card)
	}

	res, err := pegging.Score(g.pile, c.Card)
	if err != nil {
		return err // should not happen: validated above
	}
	g.emit(CardPlayed{Seat: seat, Card: c.Card, Score: res})
	if g.checkWin(seat) {
		return nil
	}
	g.afterPlay()
	return nil
}

// afterPlay resolves everything that follows a play with no decision: forced
// passes, the go/last-card point, series resets, and the show.
func (g *Game) afterPlay() {
	lp := g.lastPlayer

	if g.bothHandsEmpty() {
		if g.count != 31 {
			g.emit(GoAwarded{Seat: lp, Points: 1}) // last card
			if g.checkWin(lp) {
				return
			}
		}
		g.resolveShow()
		return
	}

	if g.count == 31 {
		g.emit(SeriesReset{Leader: g.leaderAfter(lp)})
		return
	}

	// count < 31, cards remain
	opp := other(lp)
	if !g.gone[opp] && g.canPlay(opp) {
		return // opponent's turn
	}
	if !g.gone[opp] {
		g.emit(Pass{Seat: opp}) // opponent cannot play
	}
	if g.canPlay(lp) {
		return // last player continues
	}
	// neither can play: go to the last player, then reset
	g.emit(GoAwarded{Seat: lp, Points: 1})
	if g.checkWin(lp) {
		return
	}
	g.emit(SeriesReset{Leader: g.leaderAfter(lp)})
}

// leaderAfter returns who leads the next series: the player who did not play
// last, unless they are out of cards.
func (g *Game) leaderAfter(last Seat) Seat {
	leader := other(last)
	if len(g.hands[leader]) == 0 {
		return last
	}
	return leader
}

func (g *Game) resolveShow() {
	pone := other(g.dealer)

	poneCards := g.played[pone]
	if g.emitShow(pone, poneCards) {
		return
	}
	dealerCards := g.played[g.dealer]
	if g.emitShow(g.dealer, dealerCards) {
		return
	}

	cribRes := mustHandScore(g.crib, g.starter, true)
	g.emit(CribShown{Cards: append([]cribbage.Card(nil), g.crib...), Score: cribRes})
	if g.checkWin(g.dealer) {
		return
	}

	// no winner: rotate the deal and deal the next hand
	g.dealHand(other(g.dealer))
}

func (g *Game) emitShow(seat Seat, cards []cribbage.Card) (won bool) {
	res := mustHandScore(cards, g.starter, false)
	g.emit(HandShown{Seat: seat, Cards: append([]cribbage.Card(nil), cards...), Score: res})
	return g.checkWin(seat)
}

func mustHandScore(cards []cribbage.Card, starter cribbage.Card, isCrib bool) hand.Result {
	if len(cards) != 4 {
		panic(fmt.Sprintf("game: show expects 4 cards, got %d", len(cards)))
	}
	res, err := hand.Score([4]cribbage.Card{cards[0], cards[1], cards[2], cards[3]}, starter, isCrib)
	if err != nil {
		panic(fmt.Sprintf("game: internal scoring error: %v", err))
	}
	return res
}

// checkWin emits GameWon and returns true if the seat has reached the target.
func (g *Game) checkWin(seat Seat) bool {
	if g.winner < 0 && g.scores[seat] >= g.target {
		g.emit(GameWon{Seat: seat})
		return true
	}
	return false
}

// --- turn / legality helpers --------------------------------------------------

// turn returns the seat awaiting a play. At any resting state this is the unique
// seat that can legally play.
func (g *Game) turn() Seat {
	opp := other(g.lastPlayer)
	if !g.gone[opp] && g.canPlay(opp) {
		return opp
	}
	return g.lastPlayer
}

// playable reports whether adding card c would keep the count at most 31. It is
// the single source of truth for play legality, used by canPlay, legalPlays, and
// applyPlay's validation.
func (g *Game) playable(c cribbage.Card) bool { return g.count+c.Rank.PipValue() <= 31 }

// legalPlays returns the seat's cards that keep the count at most 31.
func (g *Game) legalPlays(seat Seat) []cribbage.Card {
	var out []cribbage.Card
	for _, c := range g.hands[seat] {
		if g.playable(c) {
			out = append(out, c)
		}
	}
	return out
}

// canPlay reports whether the seat has any legal play (allocation-free, since it
// is on the turn() hot path).
func (g *Game) canPlay(seat Seat) bool {
	for _, c := range g.hands[seat] {
		if g.playable(c) {
			return true
		}
	}
	return false
}

func (g *Game) bothHandsEmpty() bool { return len(g.hands[Seat0]) == 0 && len(g.hands[Seat1]) == 0 }

// --- event sourcing -----------------------------------------------------------

// emit reduces an event into the state and appends it to the log.
func (g *Game) emit(e Event) {
	g.reduce(e)
	g.log = append(g.log, e)
	g.version++
}

// reduce is the single place state mutates. It must be a pure function of
// (current state, event) so the live state always equals the fold of the log.
func (g *Game) reduce(e Event) {
	switch e := e.(type) {
	case CutForDeal:
		g.dealer = e.Dealer

	case HandDealt:
		g.dealer = e.Dealer
		g.hands[Seat0] = append([]cribbage.Card(nil), e.Hands[Seat0]...)
		g.hands[Seat1] = append([]cribbage.Card(nil), e.Hands[Seat1]...)
		g.played = [2][]cribbage.Card{}
		g.crib = nil
		g.starter = cribbage.Card{}
		g.hasStarter = false
		g.discarded = [2]bool{}
		g.pile = nil
		g.count = 0
		g.gone = [2]bool{}
		g.lastPlayer = e.Dealer // so the non-dealer leads the play
		g.phase = PhaseDiscard

	case Discarded:
		g.hands[e.Seat] = without(g.hands[e.Seat], e.Cards[0], e.Cards[1])
		g.crib = append(g.crib, e.Cards[0], e.Cards[1])
		g.discarded[e.Seat] = true

	case StarterCut:
		g.starter = e.Card
		g.hasStarter = true
		g.scores[g.dealer] += e.Heels
		g.phase = PhasePlay

	case CardPlayed:
		g.hands[e.Seat] = without(g.hands[e.Seat], e.Card)
		g.played[e.Seat] = append(g.played[e.Seat], e.Card)
		g.pile = append(g.pile, e.Card)
		g.count += e.Card.Rank.PipValue()
		g.scores[e.Seat] += e.Score.Total
		g.lastPlayer = e.Seat

	case Pass:
		g.gone[e.Seat] = true

	case GoAwarded:
		g.scores[e.Seat] += e.Points

	case SeriesReset:
		g.pile = nil
		g.count = 0
		g.gone = [2]bool{}
		g.lastPlayer = other(e.Leader) // so turn() yields the leader

	case HandShown:
		g.scores[e.Seat] += e.Score.Total

	case CribShown:
		g.scores[g.dealer] += e.Score.Total

	case GameWon:
		g.winner = int(e.Seat)
		g.phase = PhaseComplete
	}
}

// --- accessors ----------------------------------------------------------------

// Events returns the full event log (the game's history and replay source).
func (g *Game) Events() []Event { return append([]Event(nil), g.log...) }

// Version is a monotonically increasing counter, one per event applied.
func (g *Game) Version() int { return g.version }

// Scores returns both seats' current scores.
func (g *Game) Scores() [2]int { return g.scores }

// Winner returns the winning seat and true once the game is over.
func (g *Game) Winner() (Seat, bool) {
	if g.winner < 0 {
		return 0, false
	}
	return Seat(g.winner), true
}

// --- small slice helpers ------------------------------------------------------

func contains(cards []cribbage.Card, c cribbage.Card) bool {
	for _, x := range cards {
		if x == c {
			return true
		}
	}
	return false
}

// without returns cards with the first occurrence of each removed card dropped.
func without(cards []cribbage.Card, remove ...cribbage.Card) []cribbage.Card {
	out := append([]cribbage.Card(nil), cards...)
	for _, r := range remove {
		for i, x := range out {
			if x == r {
				out = append(out[:i], out[i+1:]...)
				break
			}
		}
	}
	return out
}
