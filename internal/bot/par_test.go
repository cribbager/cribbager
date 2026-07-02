package bot

import (
	"testing"

	"github.com/cribbager/cribbager/internal/game"
)

// TestChampionPlaysAtColvertPar anchors the champion against the one external
// strength benchmark we have: Colvert's "Theory of 26" per-deal par — dealer
// ≈16.2 points/deal (hand + pegging + crib + heels), pone ≈10.2. Champion
// self-play has measured right at par (docs/research/core-assumptions-audit.md
// §2), so a drift outside the tolerance is a real behavior change.
//
// If this fails after promoting a new champion, that may be the point — a
// stronger bot can legitimately shift the split (especially pegging). Verify the
// promotion evidence, then consciously re-baseline these numbers; don't loosen
// the tolerance to make it pass.
func TestChampionPlaysAtColvertPar(t *testing.T) {
	const games = 500
	const tol = 0.3

	var dealerPts, ponePts, deals float64
	for seed := int64(0); seed < games; seed++ {
		_, events, err := PlayGameEvents(Champion(), Champion(), game.NewSeededDeck(seed))
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range DealStats(events) {
			if !d.Complete {
				continue
			}
			dealerPts += float64(d.Total(d.Dealer))
			ponePts += float64(d.Total(1 - d.Dealer))
			deals++
		}
	}

	dealerAvg := dealerPts / deals
	poneAvg := ponePts / deals
	t.Logf("complete deals %d: dealer %.2f pts/deal (par 16.2), pone %.2f pts/deal (par 10.2)",
		int(deals), dealerAvg, poneAvg)

	if dealerAvg < 16.2-tol || dealerAvg > 16.2+tol {
		t.Errorf("dealer %.2f pts/deal outside Colvert par 16.2 ± %.1f", dealerAvg, tol)
	}
	if poneAvg < 10.2-tol || poneAvg > 10.2+tol {
		t.Errorf("pone %.2f pts/deal outside Colvert par 10.2 ± %.1f", poneAvg, tol)
	}
}
