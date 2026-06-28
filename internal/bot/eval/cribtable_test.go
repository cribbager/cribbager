package eval

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestCribEVMatchesReference cross-checks the generated crib-EV table against the
// values produced by the independent TypeScript generator (src/engine/cribTable.ts
// in the former cribbager-solo project). Two languages enumerating the same domain
// and agreeing to 4 decimals validates both the table and the underlying hand
// scorer across the language boundary.
func TestCribEVMatchesReference(t *testing.T) {
	c := func(rank, suit int) cribbage.Card {
		return cribbage.Card{Rank: cribbage.Rank(rank), Suit: cribbage.Suit(suit)}
	}
	cases := []struct {
		lo, hi int
		suited bool
		want   float64
	}{
		{1, 1, false, 5.5318},   // pair of aces
		{1, 5, false, 5.7264},   // ace + five
		{4, 5, false, 6.9905},   // four + five
		{5, 5, false, 8.9927},   // pair of fives (the richest discard)
		{7, 8, false, 6.759},    // seven + eight, unsuited
		{7, 8, true, 6.8011},    // seven + eight, suited
		{10, 11, true, 5.067},   // ten + jack, suited
		{11, 11, false, 5.9286}, // pair of jacks
		{13, 13, false, 5.0322}, // pair of kings
		{1, 13, false, 3.7414},  // ace + king
	}
	for _, tc := range cases {
		suitB := 1
		if tc.suited {
			suitB = 0 // same suit as suitA below
		}
		got := CribEV(c(tc.lo, 0), c(tc.hi, suitB))
		if got != tc.want {
			t.Errorf("CribEV(%d,%d,suited=%v) = %v, want %v", tc.lo, tc.hi, tc.suited, got, tc.want)
		}
	}
}

// TestCribEVSymmetric confirms the lookup is order- and suit-identity-independent.
func TestCribEVSymmetric(t *testing.T) {
	c := func(rank, suit int) cribbage.Card {
		return cribbage.Card{Rank: cribbage.Rank(rank), Suit: cribbage.Suit(suit)}
	}
	if CribEV(c(7, 0), c(8, 1)) != CribEV(c(8, 1), c(7, 0)) {
		t.Error("CribEV not symmetric in card order")
	}
	// Suited pair of different suit identities must give the same suited value.
	if CribEV(c(7, 2), c(8, 2)) != CribEV(c(7, 0), c(8, 0)) {
		t.Error("CribEV depends on suit identity, not just sharing")
	}
}
