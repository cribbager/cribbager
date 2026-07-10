package bot

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// argmaxDiscard replicates the analyzer's contract: the bot's actual throw is
// the FIRST maximum of Value in slice order (strict greater-than scan).
func argmaxDiscard(vals []MLDiscardValue) [2]cribbage.Card {
	best, pick := vals[0].Value, vals[0].Discard
	for _, dv := range vals[1:] {
		if dv.Value > best {
			best, pick = dv.Value, dv.Discard
		}
	}
	return pick
}

// argmaxPlay is the same contract for plays: first maximum in LegalPlays order.
func argmaxPlay(vals []MLPlayValue) cribbage.Card {
	best, pick := vals[0].Value, vals[0].Card
	for _, pv := range vals[1:] {
		if pv.Value > best {
			best, pick = pv.Value, pv.Card
		}
	}
	return pick
}

// TestMLAnalyzerMatchesBot is the analyzer's core guarantee: over complete
// seeded games, the argmax of MLAnalyzer's values IS the ml bot's actual move
// at every discard and every play decision. If this holds, a game played by
// the ml bot can never be graded sub-optimal by an ml-engine analysis.
func TestMLAnalyzerMatchesBot(t *testing.T) {
	a := NewMLAnalyzer()
	b := newML()
	if a.Name() != b.Name() || a.Version() != b.Version() {
		t.Fatalf("analyzer identity %s/%s, want %s/%s", a.Name(), a.Version(), b.Name(), b.Version())
	}

	decisions := 0
	for seed := int64(1); seed <= 3; seed++ {
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed)})
		for {
			if _, over := g.Winner(); over {
				break
			}
			v := g.View(game.Seat0)
			switch v.Phase {
			case game.PhaseDiscard:
				for s := game.Seat(0); s < 2; s++ {
					vs := g.View(s)
					if len(vs.YourHand) != 6 {
						continue
					}
					pick := b.Discard(vs)
					vals := a.DiscardValues(hand6(vs.YourHand), vs.Dealer == vs.You)
					if len(vals) != 15 {
						t.Fatalf("seed %d: %d discard values, want 15", seed, len(vals))
					}
					if am := argmaxDiscard(vals); am != pick {
						t.Fatalf("seed %d: analyzer argmax %v != bot discard %v", seed, am, pick)
					}
					decisions++
					if _, err := g.Apply(s, game.Discard{Cards: pick}); err != nil {
						t.Fatal(err)
					}
				}
			case game.PhasePlay:
				s := *v.ToPlay
				vs := g.View(s)
				pick := b.Play(vs)
				vals := a.PlayValues(vs)
				if len(vals) != len(vs.LegalPlays) {
					t.Fatalf("seed %d: %d play values for %d legal plays", seed, len(vals), len(vs.LegalPlays))
				}
				if am := argmaxPlay(vals); am != pick {
					t.Fatalf("seed %d: analyzer argmax %v != bot play %v (legal %v)", seed, am, pick, vs.LegalPlays)
				}
				decisions++
				if _, err := g.Apply(s, game.Play{Card: pick}); err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatalf("unexpected phase %v", v.Phase)
			}
		}
	}
	if decisions == 0 {
		t.Fatal("no decisions exercised")
	}
}
