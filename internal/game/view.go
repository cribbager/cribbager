package game

import "github.com/cribbager/cribbager/internal/cribbage"

// PlayerView is everything a seat is permitted to see. The server sends only
// this, so hidden information (the opponent's hand, the crib before the show,
// the undealt deck) physically cannot leak. The opponent's cards are given only
// as a count; the crib only as a count until it is revealed via events at the
// show.
type PlayerView struct {
	You            Seat
	Dealer         Seat
	Phase          Phase
	Scores         [2]int
	YourHand       []cribbage.Card
	OpponentCards  int
	CribCards      int
	Starter        *cribbage.Card  // nil until the cut
	Pile           []cribbage.Card // the current count series
	YourPlayed     []cribbage.Card // cards you have played this hand (public)
	OpponentPlayed []cribbage.Card // cards the opponent has played this hand (public)
	Count          int
	ToPlay         *Seat // whose turn during the play phase; nil otherwise
	LegalPlays     []cribbage.Card
	Winner         *Seat
	Version        int
}

// View returns the visibility-filtered view for a seat.
func (g *Game) View(seat Seat) PlayerView {
	v := PlayerView{
		You:            seat,
		Dealer:         g.dealer,
		Phase:          g.phase,
		Scores:         g.scores,
		YourHand:       append([]cribbage.Card(nil), g.hands[seat]...),
		OpponentCards:  len(g.hands[other(seat)]),
		CribCards:      len(g.crib),
		Pile:           append([]cribbage.Card(nil), g.pile...),
		YourPlayed:     append([]cribbage.Card(nil), g.played[seat]...),
		OpponentPlayed: append([]cribbage.Card(nil), g.played[other(seat)]...),
		Count:          g.count,
		Version:        g.version,
	}
	if g.hasStarter {
		s := g.starter
		v.Starter = &s
	}
	if g.phase == PhasePlay {
		t := g.turn()
		v.ToPlay = &t
		if t == seat {
			v.LegalPlays = g.legalPlays(seat)
		}
	}
	if g.winner >= 0 {
		w := Seat(g.winner)
		v.Winner = &w
	}
	return v
}
