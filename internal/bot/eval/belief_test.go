package eval

import (
	"math"
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestKeepProbSanity: the generated table must reflect the discard truisms —
// 5s are kept far more often than kings when the opponent's crib is at stake
// (nobody gives away a 5), and every entry is a probability strictly inside
// (0,1) with 4 of 6 cards kept on average.
func TestKeepProbSanity(t *testing.T) {
	for role := 0; role < 2; role++ {
		var sum float64
		for r := 1; r <= 13; r++ {
			p := keepProb[role][r]
			if p <= 0 || p >= 1 {
				t.Errorf("keepProb[%d][%d] = %v, want inside (0,1)", role, r, p)
			}
			sum += p
		}
		// Ranks are not dealt exactly uniformly per hand, but across the sample
		// the mean keep rate must be 4/6.
		if avg := sum / 13; math.Abs(avg-4.0/6.0) > 0.02 {
			t.Errorf("role %d: mean keep rate %.3f, want ≈ %.3f", role, avg, 4.0/6.0)
		}
	}
	// role 0 = throwing to the opponent's crib: a 5 is almost never thrown.
	if keepProb[0][5] <= keepProb[0][13]+0.05 {
		t.Errorf("keepProb[oppCrib][5] = %.3f not clearly above [13] = %.3f",
			keepProb[0][5], keepProb[0][13])
	}
}

// TestBeliefUniformApproximatesHypergeometric: with uniform inclusion
// probabilities the independent-inclusion E[max] must track the exact
// hypergeometric model closely — the approximation error the calibrated model
// accepts by design. The bias is systematic (independence slightly understates
// the reply, since sampling without replacement concentrates the draw), so it
// shifts all candidate plays together and barely disturbs their ranking. Pool
// sizes here mirror real play states (~30-45 unseen cards); tiny pools distort
// more but do not occur in games.
func TestBeliefUniformApproximatesHypergeometric(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	deck := cribbage.Deck()
	for trial := 0; trial < 200; trial++ {
		perm := rng.Perm(52)
		pileN := 1 + rng.Intn(3)
		pile := make([]cribbage.Card, 0, pileN)
		count := 0
		for _, idx := range perm[:pileN] {
			c := deck[idx]
			if count+c.Rank.PipValue() > 26 {
				continue
			}
			pile = append(pile, c)
			count += c.Rank.PipValue()
		}
		if len(pile) == 0 {
			continue
		}
		poolN := 30 + rng.Intn(16)
		pool := make([]cribbage.Card, 0, poolN)
		for _, idx := range perm[pileN : pileN+poolN] {
			pool = append(pool, deck[idx])
		}
		h := 1 + rng.Intn(4)

		exact := ExpectedOppReply(pile, count, pool, h)
		q := make([]float64, len(pool))
		for i := range q {
			q[i] = float64(h) / float64(len(pool))
		}
		approx := ExpectedOppReplyBelief(pile, count, pool, q)
		if math.Abs(exact-approx) > 0.05+0.12*exact {
			t.Errorf("trial %d: hypergeometric %v vs independent %v (pile %v count %d hand %d)",
				trial, exact, approx, pile, count, h)
		}
		if approx > exact+0.02 {
			t.Errorf("trial %d: independent %v ABOVE hypergeometric %v — bias direction flipped",
				trial, approx, exact)
		}
	}
}

// TestBeliefMatchesBruteForce: under the independent-inclusion model the
// telescoping formula is EXACT, so it must match a brute-force sweep over all
// 2^n inclusion subsets.
func TestBeliefMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	deck := cribbage.Deck()
	for trial := 0; trial < 50; trial++ {
		perm := rng.Perm(52)
		pile := []cribbage.Card{deck[perm[0]], deck[perm[1]]}
		count := pile[0].Rank.PipValue() + pile[1].Rank.PipValue()
		pool := make([]cribbage.Card, 12)
		q := make([]float64, 12)
		for i := range pool {
			pool[i] = deck[perm[2+i]]
			q[i] = rng.Float64()
		}

		got := ExpectedOppReplyBelief(pile, count, pool, q)

		want := 0.0
		for mask := 0; mask < 1<<len(pool); mask++ {
			p := 1.0
			best := 0
			for i := range pool {
				if mask&(1<<i) != 0 {
					p *= q[i]
					if pool[i].Rank.PipValue() <= 31-count {
						if v := PlayValue(pile, pool[i]); v > best {
							best = v
						}
					}
				} else {
					p *= 1 - q[i]
				}
			}
			want += p * float64(best)
		}
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("trial %d: formula %v != brute force %v", trial, got, want)
		}
	}
}

// TestScaleToHandSize: inclusion probabilities sum to the hand size, respect
// the ≤1 clamp, and keep exact zeros at zero.
func TestScaleToHandSize(t *testing.T) {
	w := []float64{10, 1, 1, 0, 1, 1}
	q := scaleToHandSize(w, 3)
	if q[0] != 1 {
		t.Errorf("dominant weight not clamped to 1: %v", q)
	}
	if q[3] != 0 {
		t.Errorf("zero weight got probability %v", q[3])
	}
	sum := 0.0
	for _, x := range q {
		if x < 0 || x > 1 {
			t.Fatalf("q out of range: %v", q)
		}
		sum += x
	}
	if math.Abs(sum-3) > 1e-9 {
		t.Errorf("Σq = %v, want 3", sum)
	}
}

// TestOppPassConstraint: a live-series go is reconstructed from the view. The
// series ran opp 8 → me 7 → opp 9 → me 5 (count 29): with the pile ending in my
// card on my turn, the opponent must have passed at 29 and can hold nothing
// with pip ≤ 2.
func TestOppPassConstraint(t *testing.T) {
	v := game.PlayerView{
		Pile:           []cribbage.Card{card(t, "8H"), card(t, "7D"), card(t, "9S"), card(t, "5C")},
		Count:          29,
		YourPlayed:     []cribbage.Card{card(t, "7D"), card(t, "5C")},
		OpponentPlayed: []cribbage.Card{card(t, "8H"), card(t, "9S")},
		YourHand:       []cribbage.Card{card(t, "2C"), card(t, "AC")},
		LegalPlays:     []cribbage.Card{card(t, "2C"), card(t, "AC")},
	}
	maxPip, ok := oppPassConstraint(v)
	if !ok || maxPip != 2 {
		t.Fatalf("oppPassConstraint = %d,%v, want 2,true", maxPip, ok)
	}

	// Pile ending with the opponent's card carries no pass evidence.
	v2 := game.PlayerView{
		Pile:           []cribbage.Card{card(t, "8H"), card(t, "7D"), card(t, "9S")},
		Count:          24,
		YourPlayed:     []cribbage.Card{card(t, "7D")},
		OpponentPlayed: []cribbage.Card{card(t, "8H"), card(t, "9S")},
	}
	if _, ok := oppPassConstraint(v2); ok {
		t.Fatal("pass inferred from a pile ending with the opponent's card")
	}
}

// TestOppPassConstraintAgainstEngine: over thousands of real engine positions,
// whenever the view-only inference claims "the opponent passed and holds
// nothing at or below maxPip", the opponent's actual hidden hand (visible to
// the test, which plays both seats) must confirm it. A false positive here
// would poison the belief with an impossible certainty.
func TestOppPassConstraintAgainstEngine(t *testing.T) {
	checked := 0
	for seed := int64(0); seed < 300; seed++ {
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed)})
		rng := rand.New(rand.NewSource(seed))
		for {
			if _, ok := g.Winner(); ok {
				break
			}
			v0 := g.View(game.Seat0)
			switch v0.Phase {
			case game.PhaseDiscard:
				for s := game.Seat(0); s < 2; s++ {
					vs := g.View(s)
					if len(vs.YourHand) != 6 {
						continue
					}
					if _, err := g.Apply(s, game.Discard{Cards: [2]cribbage.Card{vs.YourHand[0], vs.YourHand[1]}}); err != nil {
						t.Fatal(err)
					}
				}
			case game.PhasePlay:
				s := *v0.ToPlay
				vs := g.View(s)
				if maxPip, ok := oppPassConstraint(vs); ok {
					checked++
					for _, c := range g.View(1 - s).YourHand {
						if c.Rank.PipValue() <= maxPip {
							t.Fatalf("seed %d: inferred opponent holds nothing ≤ %d, but they hold %s",
								seed, maxPip, c)
						}
					}
				}
				if _, err := g.Apply(s, game.Play{Card: vs.LegalPlays[rng.Intn(len(vs.LegalPlays))]}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no pass-constraint positions exercised")
	}
	t.Logf("verified %d inferred-pass positions against the real hidden hand", checked)
}

// TestRankPlaysLegalAndDeterministic: the calibrated play policy always
// returns a legal card and is reproducible.
func TestRankPlaysLegalAndDeterministic(t *testing.T) {
	v := game.PlayerView{
		You:            game.Seat0,
		Dealer:         game.Seat1,
		Pile:           []cribbage.Card{card(t, "TH")},
		Count:          10,
		OpponentPlayed: []cribbage.Card{card(t, "TH")},
		YourHand:       []cribbage.Card{card(t, "5D"), card(t, "9C"), card(t, "2S"), card(t, "KD")},
		YourDiscards:   []cribbage.Card{card(t, "3H"), card(t, "4H")},
		LegalPlays:     []cribbage.Card{card(t, "5D"), card(t, "9C"), card(t, "2S"), card(t, "KD")},
	}
	first := RankPlays(v)
	if len(first) != len(v.LegalPlays) {
		t.Fatalf("ranked %d plays, want %d", len(first), len(v.LegalPlays))
	}
	legal := false
	for _, c := range v.LegalPlays {
		if c == first[0].Card {
			legal = true
		}
	}
	if !legal {
		t.Fatalf("chose %s, not legal", first[0].Card)
	}
	second := RankPlays(v)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("RankPlaysCalibrated is not deterministic")
		}
	}
}
