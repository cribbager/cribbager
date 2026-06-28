package pegging

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestExhaustive walks every legal pegging sequence and checks that the
// production scorer and the independent reference agree on every play.
//
// Because a pegging score is independent of suit, the meaningful space is over
// RANK sequences: every ordering of cards (at most four of each rank, running
// count never above 31). We generate them all recursively, pruning at 31, and
// at each appended card compare the production score core against the reference
// for that play (prefix = series, new card = play).
//
// This is a genuine exhaustive proof over the entire rank space, not a sample.
// It compares the lean cores directly: input legality is guaranteed by
// construction here and is proven separately (TestRejectsIllegalPlays), and the
// Score-vs-Total agreement is covered by TestPropertiesRandom. Suits are
// assigned only to keep the physical cards distinct. Skipped under -short.
func TestExhaustive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping exhaustive pegging proof in -short mode")
	}

	var (
		rankCounts [14]int
		series     []cribbage.Card
		plays      int64
	)

	var walk func(count int)
	walk = func(count int) {
		for r := 1; r <= 13; r++ {
			pip := cribbage.Rank(r).PipValue()
			if count+pip > 31 || rankCounts[r] >= 4 {
				continue
			}
			// Assign a distinct suit for this rank's nth occurrence.
			card := cribbage.Card{Rank: cribbage.Rank(r), Suit: cribbage.Suit(rankCounts[r])}

			if got, ref := totalCore(series, card), referenceTotal(series, card); got != ref {
				t.Fatalf("differ: core=%d reference=%d for %v + %v", got, ref, series, card)
			}
			plays++

			rankCounts[r]++
			series = append(series, card)
			walk(count + pip)
			series = series[:len(series)-1]
			rankCounts[r]--
		}
	}
	walk(0)

	t.Logf("checked %d distinct legal plays", plays)
	if plays == 0 {
		t.Fatal("no plays enumerated")
	}
}
