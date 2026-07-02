// Package eval holds the two pure cribbage evaluators the bot composes: the hand
// evaluator (which two cards to discard) and the pegging evaluator (which card to
// play). Everything here is deterministic and built on the proven hand and
// pegging scorers, so it can be checked against brute-force oracles.
package eval

import (
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// remaining returns the deck cards not present in any of the excluded sets.
func remaining(excluded ...[]cribbage.Card) []cribbage.Card {
	out := make([]cribbage.Card, 0, 52)
	for _, c := range cribbage.Deck() {
		skip := false
		for _, set := range excluded {
			for _, e := range set {
				if e == c {
					skip = true
					break
				}
			}
			if skip {
				break
			}
		}
		if !skip {
			out = append(out, c)
		}
	}
	return out
}

// ExpectedHandValue is the mean score of the four kept cards over every possible
// starter — the cards not already in seen (your dealt hand). It uses the
// allocation-free hand.Total.
func ExpectedHandValue(keep [4]cribbage.Card, seen []cribbage.Card) float64 {
	starters := remaining(seen, keep[:])
	if len(starters) == 0 {
		return 0
	}
	sum := 0
	for _, s := range starters {
		t, _ := hand.Total(keep, s, false)
		sum += t
	}
	return float64(sum) / float64(len(starters))
}

// HandScoreDistSize bounds a hand score histogram: hand scores run 0..29.
const HandScoreDistSize = 30

// HandValueDist is the exact distribution of the kept four's show score over
// every possible starter — the same sweep as ExpectedHandValue, kept as a
// histogram instead of collapsed to the mean. The win-probability objective
// needs distributions: near the end of the game, a hand that scores 8-or-nothing
// and a hand that scores a flat 4 have the same mean and very different chances
// of counting out.
func HandValueDist(keep [4]cribbage.Card, seen []cribbage.Card) [HandScoreDistSize]float64 {
	var dist [HandScoreDistSize]float64
	starters := remaining(seen, keep[:])
	if len(starters) == 0 {
		return dist
	}
	for _, s := range starters {
		t, _ := hand.Total(keep, s, false)
		dist[t]++
	}
	for i := range dist {
		dist[i] /= float64(len(starters))
	}
	return dist
}

// discardPairs enumerates the 15 ways to choose two of the six dealt cards,
// returning the discard pair and the four kept cards.
func discardPairs(h [6]cribbage.Card) (discards [][2]cribbage.Card, keeps [][4]cribbage.Card) {
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			var keep [4]cribbage.Card
			n := 0
			for k := 0; k < 6; k++ {
				if k != i && k != j {
					keep[n] = h[k]
					n++
				}
			}
			discards = append(discards, [2]cribbage.Card{h[i], h[j]})
			keeps = append(keeps, keep)
		}
	}
	return discards, keeps
}
