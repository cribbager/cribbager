package ladder

import (
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestThresholdUpsidePicksMaxTail verifies the threshold-upside rung actually
// maximizes P(hand >= T) over the 15 holds (its own objective), and that it is a
// DISTINCT policy from the mean pick — i.e. at a high threshold it diverges from
// eval.ExpectedHandValue on real hands, so the desperation experiment is
// measuring something real, not the mean under a different name.
func TestThresholdUpsidePicksMaxTail(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	full := cribbage.Deck()
	thresholds := []int{8, 12, 16}
	divergedFromMean := map[int]bool{}

	for n := 0; n < 400; n++ {
		deck := append([]cribbage.Card(nil), full...)
		h := deal6(deck, rng)
		for _, dealer := range []bool{false, true} {
			sp := Splits(h, dealer)
			meanPick := pickMaxEHand(sp)
			for _, T := range thresholds {
				pick := ThresholdUpsideRung(T).Discard(h, dealer, 0, 0)

				// The rung's pick must have the maximum tail probability.
				var maxP float64
				for _, s := range sp {
					if p := tailProb(s.Keep, h[:], T); p > maxP {
						maxP = p
					}
				}
				got := tailProb(findSplit(sp, pick).Keep, h[:], T)
				if got < maxP-1e-9 {
					t.Fatalf("upside>=%d picked tail %.5f but the max over holds is %.5f (hand %v)",
						T, got, maxP, h)
				}
				if pick != meanPick {
					divergedFromMean[T] = true
				}
			}
		}
	}
	// The whole point of the rung is that chasing tail mass differs from chasing
	// the mean — most visibly at the high threshold.
	if !divergedFromMean[16] {
		t.Error("upside>=16 never diverged from the mean pick over 400 hands — not a distinct policy?")
	}
}
