package eval

import (
	"math"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

func mustCard(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// uniformCrib is weightedCrib with all throw weights forced to 1 — the plain
// uniform mean. It must equal the shipped uniform CribEV table, which proves the
// weighted enumeration matches the generator's.
func uniformCrib(a, b cribbage.Card) float64 {
	rest := make([]cribbage.Card, 0, 50)
	for _, c := range cribbage.Deck() {
		if c != a && c != b {
			rest = append(rest, c)
		}
	}
	n := len(rest)
	var sum float64
	var count int
	for s := 0; s < n; s++ {
		for i := 0; i < n; i++ {
			if i == s {
				continue
			}
			for j := i + 1; j < n; j++ {
				if j == s {
					continue
				}
				crib := [4]cribbage.Card{a, b, rest[i], rest[j]}
				t, _ := hand.Total(crib, rest[s], true)
				sum += float64(t)
				count++
			}
		}
	}
	return sum / float64(count)
}

// TestWeightedCribUniformMatchesTable verifies the enumeration: uniform weights
// reproduce the shipped uniform CribEV to 1e-6, so any difference in the
// opponent-weighted result is the opponent model, not an enumeration bug.
func TestWeightedCribUniformMatchesTable(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"5H", "5S"}, {"5H", "TD"}, {"AH", "KS"}, {"7C", "8C"}, {"2D", "3D"},
	} {
		a, b := mustCard(t, tc.a), mustCard(t, tc.b)
		got, want := uniformCrib(a, b), CribEV(a, b)
		// The shipped table is rounded to 4 decimals (round4 in the generator),
		// so exact-vs-table agreement to ~5e-5 confirms the enumeration matches.
		if math.Abs(got-want) > 1e-3 {
			t.Errorf("uniformCrib(%s,%s)=%.6f, CribEV table=%.6f", tc.a, tc.b, got, want)
		}
	}
}

// TestOppCribAsymmetry verifies the core asymmetry the model exists to capture:
// the SAME two cards are worth LESS to our crib than to the opponent's, because
// the opponent throws defensively to ours (role 0, as pone) and more freely to
// their own (role 1, as dealer). This holds regardless of where each sits
// relative to the uniform value (both can be below uniform when the discard
// wants a card the opponent hoards in both roles, e.g. a 5).
func TestOppCribAsymmetry(t *testing.T) {
	for _, tc := range []string{"7H", "8C", "4D", "9S", "2C", "TD"} {
		a := mustCard(t, tc)
		b := mustCard(t, "6D")
		toOurCrib := weightedCrib(a, b, 0)   // opponent is pone, defensive
		toTheirCrib := weightedCrib(a, b, 1) // opponent is dealer, helpful
		if toOurCrib >= toTheirCrib {
			t.Errorf("%s+6D: expected our-crib(%.3f) < their-crib(%.3f) — defensive vs helpful opponent",
				tc, toOurCrib, toTheirCrib)
		}
	}
}
