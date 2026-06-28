package eval

import (
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

func deal6(rng *rand.Rand) [6]cribbage.Card {
	d := cribbage.Deck()
	rng.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
	var h [6]cribbage.Card
	copy(h[:], d[:6])
	return h
}

func inSet(c cribbage.Card, sets ...[]cribbage.Card) bool {
	for _, s := range sets {
		for _, x := range s {
			if x == c {
				return true
			}
		}
	}
	return false
}

// bruteExpectedHand re-derives the expected hand value independently, using the
// itemized hand.Score (rather than hand.Total) over a plain deck scan.
func bruteExpectedHand(keep [4]cribbage.Card, seen []cribbage.Card) float64 {
	sum, n := 0, 0
	for _, s := range cribbage.Deck() {
		if inSet(s, seen, keep[:]) {
			continue
		}
		r, _ := hand.Score(keep, s, false)
		sum += r.Total
		n++
	}
	return float64(sum) / float64(n)
}

func TestExpectedHandValueMatchesBrute(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		h := deal6(rng)
		keep := [4]cribbage.Card{h[0], h[1], h[2], h[3]}
		got := ExpectedHandValue(keep, h[:])
		want := bruteExpectedHand(keep, h[:])
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("ExpectedHandValue=%v, brute=%v for %v", got, want, keep)
		}
		if got < 0 || got > 29 {
			t.Fatalf("expected value %v out of [0,29]", got)
		}
	}
}

// TestRankDiscardsTopIsArgmax checks the canonical discard ranking puts the
// max-EV hold first (the hand-EV component is independently re-derivable).
func TestRankDiscardsTopIsArgmax(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 1000; i++ {
		h := deal6(rng)
		ranked := RankDiscards(h, true)
		top := ranked[0].Score
		for _, r := range ranked {
			if r.Score > top+1e-9 {
				t.Fatalf("RankDiscards not sorted: %.4f after top %.4f", r.Score, top)
			}
		}
	}
}
