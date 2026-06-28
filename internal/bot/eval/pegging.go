package eval

import (
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// PlayValue is the immediate points a card scores when added to the pile.
func PlayValue(pile []cribbage.Card, card cribbage.Card) int {
	r, _ := pegging.Score(pile, card)
	return r.Total
}

// unseen returns the cards this seat cannot see: everything except its own hand,
// the starter, and every card already played face-up this hand (both seats —
// the current pile is only the live count series, so earlier series are picked
// up via YourPlayed/OpponentPlayed). This leaves exactly the opponent's hidden
// hand plus the undealt stock, which is the right pool for the reply estimate.
func unseen(v game.PlayerView) []cribbage.Card {
	known := map[cribbage.Card]bool{}
	for _, c := range v.YourHand {
		known[c] = true
	}
	for _, c := range v.Pile {
		known[c] = true
	}
	for _, c := range v.YourPlayed {
		known[c] = true
	}
	for _, c := range v.OpponentPlayed {
		known[c] = true
	}
	if v.Starter != nil {
		known[*v.Starter] = true
	}
	out := make([]cribbage.Card, 0, 52)
	for _, c := range cribbage.Deck() {
		if !known[c] {
			out = append(out, c)
		}
	}
	return out
}
