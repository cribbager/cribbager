package eval

import (
	"math"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestProbHoldsNone(t *testing.T) {
	cases := []struct {
		U, k, h int
		want    float64
	}{
		{10, 0, 3, 1},                      // nothing flagged → always holds none
		{10, 2, 1, 0.8},                    // 1 draw avoids 2 of 10 → 8/10
		{10, 2, 2, (8.0 / 10) * (7.0 / 9)}, // 2 draws avoid both
		{3, 3, 1, 0},                       // every card flagged → impossible to hold none
		{5, 6, 1, 0},                       // more flagged than the pool
	}
	for _, tc := range cases {
		if got := probHoldsNone(tc.U, tc.k, tc.h); !approx(got, tc.want) {
			t.Errorf("probHoldsNone(%d,%d,%d) = %v, want %v", tc.U, tc.k, tc.h, got, tc.want)
		}
	}
}

func TestExpectedOppReply(t *testing.T) {
	c := func(rank int, suit cribbage.Suit) cribbage.Card {
		return cribbage.Card{Rank: cribbage.Rank(rank), Suit: suit}
	}
	pile := []cribbage.Card{c(5, cribbage.Clubs)} // count is 5; a ten-card makes 15

	// One opponent card, the only unseen card scores 2 (15-for-2): reply is exactly 2.
	if got := ExpectedOppReply(pile, 5, []cribbage.Card{c(10, cribbage.Diamonds)}, 1); !approx(got, 2) {
		t.Errorf("single scoring card: got %v, want 2", got)
	}

	// Three unseen cards, two score 2 (a ten for 15, a five for the pair) and one
	// scores nothing; opponent holds 1. P(a scoring card) = 2/3, each worth 2 →
	// E[reply] = 4/3.
	unseen := []cribbage.Card{c(10, cribbage.Diamonds), c(5, cribbage.Diamonds), c(2, cribbage.Clubs)}
	if got := ExpectedOppReply(pile, 5, unseen, 1); !approx(got, 4.0/3.0) {
		t.Errorf("mixed unseen: got %v, want %v", got, 4.0/3.0)
	}

	// No opponent cards → no reply.
	if got := ExpectedOppReply(pile, 5, unseen, 0); got != 0 {
		t.Errorf("empty hand: got %v, want 0", got)
	}
}
