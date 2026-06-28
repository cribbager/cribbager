package hand

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestExhaustive is the core correctness proof. It walks every one of the
// 12,994,800 distinct (4-card hand, starter) situations and checks:
//
//   - production Total agrees with the independent referenceTotal on all of them
//     (differential testing — two unrelated algorithms over the whole space);
//   - Score's total agrees with Total everywhere;
//   - the resulting score distribution has the structural properties cribbage
//     guarantees: range 0..29, the four impossible scores (19, 25, 26, 27) never
//     occur, and the maximum 29 does occur.
//
// The full histogram is logged so it can be eyeballed against the published
// distribution during review.
//
// It is skipped under `go test -short`; run the full proof with plain
// `go test ./internal/scoring/hand`.
func TestExhaustive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping exhaustive scoring proof in -short mode")
	}

	deck := cribbage.Deck()
	var hist [30]int64
	var total int64

	// Choose 5 distinct cards (a, b, c, d, e), then let each of the five be the
	// starter in turn, with the other four forming the hand. That enumerates all
	// 52*C(51,4) = 12,994,800 hand+starter situations exactly once.
	for a := 0; a < 52; a++ {
		for b := a + 1; b < 52; b++ {
			for c := b + 1; c < 52; c++ {
				for d := c + 1; d < 52; d++ {
					for e := d + 1; e < 52; e++ {
						idx := [5]int{a, b, c, d, e}
						for s := 0; s < 5; s++ {
							var h [4]cribbage.Card
							n := 0
							for k := 0; k < 5; k++ {
								if k == s {
									continue
								}
								h[n] = deck[idx[k]]
								n++
							}
							starter := deck[idx[s]]

							got, err := Total(h, starter, false)
							if err != nil {
								t.Fatalf("Total error on %v + %v: %v", h, starter, err)
							}
							if ref := referenceTotal(h, starter, false); ref != got {
								t.Fatalf("differ: Total=%d reference=%d for %v + %v", got, ref, h, starter)
							}
							if sc, _ := Score(h, starter, false); sc.Total != got {
								t.Fatalf("Score.Total=%d != Total=%d for %v + %v", sc.Total, got, h, starter)
							}

							if got < 0 || got > 29 {
								t.Fatalf("score %d out of range for %v + %v", got, h, starter)
							}
							hist[got]++
							total++
						}
					}
				}
			}
		}
	}

	if total != 12_994_800 {
		t.Fatalf("enumerated %d situations, want 12,994,800", total)
	}

	// publishedDistribution is the canonical frequency of each hand score over
	// the 12,994,800 enumeration (an external oracle, independent of our code).
	// Matching it bucket-for-bucket — including the four zeros at 19/25/26/27 —
	// is far stronger than any spot check.
	publishedDistribution := [30]int64{
		1009008, 99792, 2813796, 505008, 2855676, 697508, 1800268, 751324,
		1137236, 361224, 388740, 51680, 317340, 19656, 90100, 9168, 58248,
		11196, 2708, 0, 8068, 2496, 444, 356, 3680, 0, 0, 0, 76, 4,
	}
	if hist != publishedDistribution {
		for s := range hist {
			if hist[s] != publishedDistribution[s] {
				t.Errorf("score %d: got %d, published %d", s, hist[s], publishedDistribution[s])
			}
		}
	}

	var weighted int64
	for s, n := range hist {
		weighted += int64(s) * n
	}
	t.Logf("mean = %.4f over %d hands", float64(weighted)/float64(total), total)
}
