package ladder

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// card parses a two-character notation ("5H", "TD", "JS") into a Card, failing
// the test on bad input. Keeps the fixtures readable.
func card(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatalf("card(%q): %v", s, err)
	}
	return c
}

func keep4(t *testing.T, a, b, c, d string) [4]cribbage.Card {
	t.Helper()
	return [4]cribbage.Card{card(t, a), card(t, b), card(t, c), card(t, d)}
}

// TestShowValueNoStarter pins the four-card no-starter scorer against
// hand-computed values covering fifteens, pairs, runs (including a double run),
// a four-card flush (scores 4, never 5), and the empty hand.
func TestShowValueNoStarter(t *testing.T) {
	cases := []struct {
		name       string
		a, b, c, d string
		want       int
	}{
		// Four fives: pairs C(4,2)=6 => 12, plus four triples summing to 15 => 8.
		// Different suits, so no flush.
		{"four-fives", "5C", "5D", "5H", "5S", 20},
		// 4-5-6-7 all clubs: run4 (4) + fifteen {4,5,6} (2) + four-card flush (4).
		{"straight-flush4", "4C", "5C", "6C", "7C", 10},
		// Same ranks, mixed suits: run4 (4) + fifteen (2), no flush.
		{"straight-mixed", "4C", "5D", "6H", "7S", 6},
		// Double run of three A-2-2-3: run 1-2-3 x2 (6) + the pair of 2s (2) = 8.
		{"double-run", "AC", "2C", "2D", "3C", 8},
		// Four jacks: pairs (12); J pips are 10 so no fifteen; mixed suits.
		{"four-jacks", "JC", "JD", "JH", "JS", 12},
		// A-2-3-4: run4 only (no fifteen: 1+2+3+4=10).
		{"run4-nofifteen", "AC", "2D", "3H", "4S", 4},
		// 5-5-5-T: fifteens {5,T}x3 (6) + {5,5,5} (2) = 8, plus pairs of fives (6).
		{"fives-and-ten", "5C", "5D", "5H", "TC", 14},
		// Nothing: no fifteen, no pair, no run, no flush.
		{"nothing", "2C", "4D", "6H", "8S", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ShowValueNoStarter(keep4(t, tc.a, tc.b, tc.c, tc.d))
			if got != tc.want {
				t.Errorf("ShowValueNoStarter(%s %s %s %s) = %d, want %d",
					tc.a, tc.b, tc.c, tc.d, got, tc.want)
			}
		})
	}
}

// TestCeilingDivergesFromMean pins the ceiling rung apart from L2: on a hand
// where the luckiest cut favors one keep but steady value favors another, the
// max-over-starters pick and the mean-over-starters pick are different keeps —
// and each strictly wins on its own objective (the crossover). Hand found by an
// exhaustive sweep; the picks are stable and score-independent.
func TestCeilingDivergesFromMean(t *testing.T) {
	// 5S 4S QD 3S AS 6D: keeping 3-4-5-6 chases a luckiest cut of 16 (mean 9.87);
	// keeping A-3-4-5 (four spades — a flush) has the higher expected value 11.24
	// but a lower ceiling (14). The ceiling and mean rungs split here.
	h := [6]cribbage.Card{
		card(t, "5S"), card(t, "4S"), card(t, "QD"),
		card(t, "3S"), card(t, "AS"), card(t, "6D"),
	}
	sp := Splits(h, false) // EHand/Ceil are score- and dealer-independent

	ceilPick := pickMaxCeil(sp)
	meanPick := pickMaxEHand(sp)
	if ceilPick == meanPick {
		t.Fatalf("expected the ceiling and mean picks to differ, both chose %v", ceilPick)
	}

	ceilSplit := findSplit(sp, ceilPick)
	meanSplit := findSplit(sp, meanPick)

	// The ceiling keep has the strictly higher luckiest cut...
	if ceilSplit.Ceil <= meanSplit.Ceil {
		t.Errorf("ceiling pick %v (Ceil=%.0f) should beat mean pick %v (Ceil=%.0f) on the max cut",
			ceilPick, ceilSplit.Ceil, meanPick, meanSplit.Ceil)
	}
	// ...while the mean keep has the strictly higher expected value — so the
	// greedy ceiling pick throws away expected value chasing the spike.
	if meanSplit.EHand <= ceilSplit.EHand {
		t.Errorf("mean pick %v (E=%.3f) should beat ceiling pick %v (E=%.3f) on expected value",
			meanPick, meanSplit.EHand, ceilPick, ceilSplit.EHand)
	}
	t.Logf("ceiling keep %v: Ceil=%.0f E=%.3f | mean keep %v: Ceil=%.0f E=%.3f",
		ceilSplit.Keep, ceilSplit.Ceil, ceilSplit.EHand,
		meanSplit.Keep, meanSplit.Ceil, meanSplit.EHand)
}

// TestShowValueMatchesHandTotalWithDeadStarter cross-checks the no-starter
// scorer against the proven five-card hand.Total: for a keep with no jack and a
// DEAD starter (a card that forms no fifteen, pair, run, flush, or nobs with the
// keep), the five-card total must equal the four-card intrinsic value.
func TestShowValueMatchesHandTotalWithDeadStarter(t *testing.T) {
	cases := []struct {
		name        string
		a, b, c, d  string
		deadStarter string
	}{
		// Ace of clubs is dead against 4-5-6-7 (mixed suits): 1 makes no 15 with
		// any subset, is not adjacent to the run, pairs nothing, no flush.
		{"mixed-run", "4C", "5D", "6H", "7S", "AC"},
		// Same four ranks all clubs, starter a heart: the four-card flush stands
		// (scores 4; the starter's different suit means no five-card flush).
		{"clubs-flush", "4C", "5C", "6C", "7C", "AH"},
		// A seven is dead against 2-3-4-9 (mixed): 15−7=8 is not a subset sum of
		// {2,3,4,9}, 7 is not adjacent to the 2-3-4 run, pairs nothing, no flush.
		{"scattered", "2C", "3D", "4H", "9S", "7C"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep := keep4(t, tc.a, tc.b, tc.c, tc.d)
			starter := card(t, tc.deadStarter)
			total, err := hand.Total(keep, starter, false)
			if err != nil {
				t.Fatalf("hand.Total: %v", err)
			}
			got := ShowValueNoStarter(keep)
			if got != total {
				t.Errorf("ShowValueNoStarter=%d but hand.Total with dead starter %s=%d (not dead?)",
					got, tc.deadStarter, total)
			}
		})
	}
}
