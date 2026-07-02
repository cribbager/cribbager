package bot

import (
	"math/rand"
	"testing"
)

// A deterministic bot against itself on duplicate deals plays the identical game
// twice, so whichever seat wins in game 1 also wins in game 2 — every pair
// splits, and both paired instruments read exactly zero.
func TestCompareSelfPlaySplitsEveryPair(t *testing.T) {
	c, err := Compare(Champion(), Champion(), 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.PairsSplit != c.Pairs || c.PairsAWinBoth != 0 || c.PairsBWinBoth != 0 {
		t.Errorf("self-play pairs: want all %d split, got A-both %d / split %d / B-both %d",
			c.Pairs, c.PairsAWinBoth, c.PairsSplit, c.PairsBWinBoth)
	}
	if c.WinDiff != 0 {
		t.Errorf("self-play WinDiff = %v, want 0", c.WinDiff)
	}
	if c.Margin != 0 {
		t.Errorf("self-play Margin = %v, want 0", c.Margin)
	}
}

// The champion is far stronger than legal-random, so the paired win-difference
// CI must clear zero even at a modest pair count — the smoke test that the win
// instrument has power.
func TestCompareWinDiffSeparatesChampionFromRandom(t *testing.T) {
	c, err := Compare(Champion(), NewRandom(rand.New(rand.NewSource(1))), 50, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.WinDiffCILo <= 0 {
		t.Errorf("champion vs random WinDiff CI [%.3f, %.3f] does not clear zero (WinDiff %.3f)",
			c.WinDiffCILo, c.WinDiffCIHi, c.WinDiff)
	}
	if got := c.PairsAWinBoth + c.PairsSplit + c.PairsBWinBoth; got != c.Pairs {
		t.Errorf("pair classes sum to %d, want %d", got, c.Pairs)
	}
}
