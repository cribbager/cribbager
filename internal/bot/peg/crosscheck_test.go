// External test package: it imports internal/bot, which imports peg, so this
// cross-check cannot live inside package peg without an import cycle.
package peg_test

import (
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/peg"
	"github.com/cribbager/cribbager/internal/game"
)

// TestPegTotalsMatchDealStats holds peg's event walker equal to bot.DealStats,
// the independently written extractor: per deal and seat, the summed pegging
// points must agree exactly. This is what lets Generate's returns be trusted —
// two readers of the same event log, one meaning of "pegging points".
func TestPegTotalsMatchDealStats(t *testing.T) {
	g := game.New(game.Options{Deck: game.NewSeededDeck(7)})
	rng := rand.New(rand.NewSource(7))
	discarder := bot.Champion()
	for {
		if _, over := g.Winner(); over {
			break
		}
		v := g.View(game.Seat0)
		switch v.Phase {
		case game.PhaseDiscard:
			for s := game.Seat(0); s < 2; s++ {
				if vs := g.View(s); len(vs.YourHand) == 6 {
					if _, err := g.Apply(s, game.Discard{Cards: discarder.Discard(vs)}); err != nil {
						t.Fatal(err)
					}
				}
			}
		case game.PhasePlay:
			seat := *v.ToPlay
			if _, err := g.Apply(seat, game.Play{Card: peg.Random{}.Play(g.View(seat), rng)}); err != nil {
				t.Fatal(err)
			}
		}
	}

	mine := peg.PegTotals(g.Events())
	ref := bot.DealStats(g.Events())
	if len(mine) != len(ref) {
		t.Fatalf("deal count: PegTotals %d, DealStats %d", len(mine), len(ref))
	}
	for d := range mine {
		if mine[d] != ref[d].Peg {
			t.Errorf("deal %d: PegTotals %v, DealStats %v", d, mine[d], ref[d].Peg)
		}
	}
}
