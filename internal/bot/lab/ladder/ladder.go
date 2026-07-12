// Package ladder is a research testbed: a LADDER of cribbage discard evaluators
// of increasing sophistication, wired so each added concept can be measured in
// isolation. It exists to answer "how much does each idea in discard evaluation
// actually buy?" — not to ship a bot (nothing here is registered as a
// production bot; the experiments live behind an opt-in go-test gate).
//
// The rungs, floor to ceiling:
//
//	L0 random     — a seeded uniform-random legal discard (the floor).
//	L1 max-hand   — keep the four cards with the highest INTRINSIC (no-starter)
//	                show value. Greedy: no starter sweep, no crib.
//	L2 exp-hand   — argmax of the exact expected hand value over all 46 starters
//	                (eval.ExpectedHandValue). Still no crib.
//	L3 point-EV   — argmax of expected hand value PLUS the signed uniform-opponent
//	                crib EV (eval.RankDiscards) — today's non-win-aware evaluator.
//	L4 champion   — the shipped champion's win-aware discard (eval.RankDiscardsWin),
//	                which reduces to L3 away from the endgame.
//
// Adding a rung is a one-liner in Ladder (see the owner's planned
// "opponent-modeled crib" rung): give it a Name and a Discard function.
package ladder

import (
	"fmt"
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// ShowValueNoStarter is the intrinsic show value of four kept cards with NO
// starter: fifteens + pairs + runs + a four-card flush (all four same suit = 4).
// There is deliberately no nobs (it needs the starter) and no five-card flush
// (there is no fifth card). It mirrors the lean hand.totalOf scorer restricted
// to four cards — pairs are counted traditionally and runs as length*multiplicity,
// so a duplicated rank inside a run contributes both the run and its pair, exactly
// as the five-card scorer does. This is L1's objective.
func ShowValueNoStarter(keep [4]cribbage.Card) int {
	total := 0

	// Fifteens: every non-empty subset of the four cards summing to 15.
	for mask := 1; mask < 16; mask++ {
		sum := 0
		for i := 0; i < 4; i++ {
			if mask&(1<<i) != 0 {
				sum += keep[i].Rank.PipValue()
			}
		}
		if sum == 15 {
			total += 2
		}
	}

	var counts [14]int // index by rank 1..13
	for _, c := range keep {
		counts[c.Rank]++
	}

	// Pairs: 2 points per unordered same-rank pair => count*(count-1).
	for r := 1; r <= 13; r++ {
		total += counts[r] * (counts[r] - 1)
	}

	// Runs: each maximal run of >=3 consecutive ranks scores length*multiplicity.
	for r := 1; r <= 13; {
		if counts[r] == 0 {
			r++
			continue
		}
		start := r
		for r+1 <= 13 && counts[r+1] > 0 {
			r++
		}
		if length := r - start + 1; length >= 3 {
			mult := 1
			for k := start; k <= r; k++ {
				mult *= counts[k]
			}
			total += length * mult
		}
		r++
	}

	// Four-card flush: all four the same suit scores 4 (never 5 — no starter).
	if keep[0].Suit == keep[1].Suit && keep[1].Suit == keep[2].Suit && keep[2].Suit == keep[3].Suit {
		total += 4
	}

	return total
}

// Split is one of the 15 ways to hold four of the six dealt cards, with the
// scalar objectives each ladder rung maximizes precomputed once so a rung is a
// single argmax and the experiment harness never recomputes.
type Split struct {
	Discard [2]cribbage.Card
	Keep    [4]cribbage.Card
	Show    int     // L1: intrinsic no-starter show value of the keep
	EHand   float64 // L2: exact mean hand value over all 46 starters
	Ceil    float64 // L1c: BEST-case hand value — max over the 46 starters (the luckiest cut)
	Crib    float64 // signed uniform-opponent crib EV of the discard (+dealer, −pone)
	Score   float64 // L3: EHand + Crib
}

// startersFor returns the 46 possible starters for a dealt six-card hand: the
// deck minus the six cards. Every keep drawn from this hand sweeps the same 46
// starters (the kept four are a subset of the six), so the harness builds this
// once per hand and shares it across all 15 holds.
func startersFor(h [6]cribbage.Card) []cribbage.Card {
	out := make([]cribbage.Card, 0, 46)
	for _, c := range cribbage.Deck() {
		inHand := false
		for _, hc := range h {
			if hc == c {
				inHand = true
				break
			}
		}
		if !inHand {
			out = append(out, c)
		}
	}
	return out
}

// meanMaxHandValue sweeps the given starters once, returning both the MEAN
// (L2's objective, identical to eval.ExpectedHandValue over the same set) and
// the MAX (L1c's objective — the single luckiest cut). One pass so the ceiling
// costs no extra starter sweep beyond the mean the harness already pays.
func meanMaxHandValue(keep [4]cribbage.Card, starters []cribbage.Card) (mean, ceil float64) {
	if len(starters) == 0 {
		return 0, 0
	}
	sum, mx := 0, 0
	for _, s := range starters {
		t, _ := hand.Total(keep, s, false)
		sum += t
		if t > mx {
			mx = t
		}
	}
	return float64(sum) / float64(len(starters)), float64(mx)
}

// Splits enumerates all 15 holds in the canonical discardPairs order (i<j over
// the six cards), computing each rung's objective. The 46-starter sweep is done
// once per hold (yielding both the mean and the max), so L1c, L2, and L3 share
// one pass.
func Splits(h [6]cribbage.Card, dealer bool) []Split {
	sign := -1.0
	if dealer {
		sign = 1.0
	}
	starters := startersFor(h)
	out := make([]Split, 0, 15)
	for i := 0; i < 6; i++ {
		for j := i + 1; j < 6; j++ {
			var keep [4]cribbage.Card
			n := 0
			for k := 0; k < 6; k++ {
				if k != i && k != j {
					keep[n] = h[k]
					n++
				}
			}
			eHand, ceil := meanMaxHandValue(keep, starters)
			crib := sign * eval.CribEV(h[i], h[j])
			out = append(out, Split{
				Discard: [2]cribbage.Card{h[i], h[j]},
				Keep:    keep,
				Show:    ShowValueNoStarter(keep),
				EHand:   eHand,
				Ceil:    ceil,
				Crib:    crib,
				Score:   eHand + crib,
			})
		}
	}
	return out
}

// The three argmaxes over the shared splits. Each keeps enumeration order on
// ties (strict >, first maximum wins) so it matches eval.RankDiscards' stable
// top pick and the choice is reproducible. Both the rung Discard closures and
// the agreement experiment go through these, so they can never disagree.

func pickMaxShow(sp []Split) [2]cribbage.Card {
	best := 0
	for i := 1; i < len(sp); i++ {
		if sp[i].Show > sp[best].Show {
			best = i
		}
	}
	return sp[best].Discard
}

func pickMaxEHand(sp []Split) [2]cribbage.Card {
	best := 0
	for i := 1; i < len(sp); i++ {
		if sp[i].EHand > sp[best].EHand {
			best = i
		}
	}
	return sp[best].Discard
}

func pickMaxCeil(sp []Split) [2]cribbage.Card {
	best := 0
	for i := 1; i < len(sp); i++ {
		if sp[i].Ceil > sp[best].Ceil {
			best = i
		}
	}
	return sp[best].Discard
}

func pickMaxScore(sp []Split) [2]cribbage.Card {
	best := 0
	for i := 1; i < len(sp); i++ {
		if sp[i].Score > sp[best].Score {
			best = i
		}
	}
	return sp[best].Discard
}

// Discarder is one rung: a named pure discard policy. Scores are supplied to
// every rung but ignored by all but L4, so a new rung slots in without changing
// the harness.
type Discarder struct {
	Name    string
	Discard func(h [6]cribbage.Card, dealer bool, myScore, oppScore int) [2]cribbage.Card
}

// Rung indices into the slice returned by Ladder, for callers that want to name
// a specific rung (e.g. "agreement with the top").
const (
	L0 = 0
	L1 = 1
	L2 = 2
	L3 = 3
	L4 = 4
)

// Ladder builds the rungs floor-to-ceiling. seed seeds L0's uniform-random
// discard; the deterministic rungs ignore it. Returned fresh each call so a
// caller gets an independent L0 RNG stream.
func Ladder(seed int64) []Discarder {
	rng := rand.New(rand.NewSource(seed))
	return []Discarder{
		{
			Name: "L0-random",
			Discard: func(h [6]cribbage.Card, _ bool, _, _ int) [2]cribbage.Card {
				i := rng.Intn(6)
				j := rng.Intn(5)
				if j >= i {
					j++
				}
				if i > j { // canonical i<j order, matching Splits' enumeration
					i, j = j, i
				}
				return [2]cribbage.Card{h[i], h[j]}
			},
		},
		{
			Name: "L1-maxhand",
			Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
				return pickMaxShow(Splits(h, dealer))
			},
		},
		{
			Name: "L2-exphand",
			Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
				return pickMaxEHand(Splits(h, dealer))
			},
		},
		{
			Name: "L3-pointEV",
			Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
				return pickMaxScore(Splits(h, dealer))
			},
		},
		{
			Name: "L4-champion",
			Discard: func(h [6]cribbage.Card, dealer bool, my, opp int) [2]cribbage.Card {
				d, _ := eval.BestDiscardWin(h, dealer, my, opp)
				return d
			},
		},
	}
}

// CeilingRung is the greedy best-case evaluator — an OFF-LADDER rung that sits
// conceptually beside L1/L2. It keeps the four cards whose single luckiest cut
// is highest: argmax over the 15 holds of max-over-46-starters hand value. It is
// NOT part of the monotonic Ladder (so Experiment 2's chain is unchanged); it is
// a named policy the agreement experiment measures against L1, L2, and L4. The
// contrast with L2 (which maximizes the MEAN over the same 46 starters) is the
// point: the ceiling chases an unlikely spike, L2 chases steady value.
func CeilingRung() Discarder {
	return Discarder{
		Name: "L1c-ceiling",
		Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
			return pickMaxCeil(Splits(h, dealer))
		},
	}
}

// tailProb is P(hand score >= T) for the kept four over all 46 starters — the
// exact right-tail mass of eval.HandValueDist. T<=0 is the whole distribution
// (probability 1). This is the CORRECT formalization of "upside": the chance of
// clearing a needed count, not the single luckiest cut the ceiling chases.
func tailProb(keep [4]cribbage.Card, seen []cribbage.Card, T int) float64 {
	if T <= 0 {
		return 1
	}
	dist := eval.HandValueDist(keep, seen)
	p := 0.0
	for k := T; k < len(dist); k++ {
		p += dist[k]
	}
	return p
}

// pickMaxTail is the argmax of tailProb over the 15 holds, ties broken toward
// the higher mean hand value (so it degrades to the sensible pick when the tail
// is flat) and then enumeration order — deterministic like the other picks.
func pickMaxTail(sp []Split, seen []cribbage.Card, T int) [2]cribbage.Card {
	best := 0
	bestP := tailProb(sp[0].Keep, seen, T)
	for i := 1; i < len(sp); i++ {
		p := tailProb(sp[i].Keep, seen, T)
		if p > bestP+1e-12 {
			best, bestP = i, p
		} else if p > bestP-1e-12 && sp[i].EHand > sp[best].EHand {
			best = i
		}
	}
	return sp[best].Discard
}

// ThresholdUpsideRung is the correctly-formalized "go for a big hand" evaluator:
// it keeps the four cards that MAXIMIZE THE PROBABILITY the hand scores at least
// T over the cut, P(hand >= T). Unlike the ceiling (which maximizes the single
// luckiest starter and so rewards a hand that spikes once and busts otherwise),
// this rewards a hand that clears the threshold OFTEN — which is what actually
// wins games when you need points. An off-ladder rung; the desperation
// experiment measures whether the champion shifts toward high-T picks when it is
// behind and out of time.
func ThresholdUpsideRung(T int) Discarder {
	return Discarder{
		Name: fmt.Sprintf("upside>=%d", T),
		Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
			return pickMaxTail(Splits(h, dealer), h[:], T)
		},
	}
}

// OppCribScore is the point-EV score of a split under the OPPONENT-MODELED crib:
// exact expected hand value plus the signed opponent-weighted crib EV
// (eval.OppCribEV) instead of L3's uniform-opponent crib. sign is +1 for our
// crib (dealer), −1 for the opponent's.
func OppCribScore(s Split, dealer bool) float64 {
	sign := -1.0
	if dealer {
		sign = 1.0
	}
	return s.EHand + sign*eval.OppCribEV(s.Discard[0], s.Discard[1], dealer)
}

// OppCribRung is L3 with the uniform crib table swapped for the opponent-modeled
// one — the only change from L3-pointEV. Comparing the two (agreement, and a
// discard-isolated head-to-head) answers whether the standard uniform-opponent
// crib assumption leaves points on the table. Off-ladder (it forks L3, not L4).
// The first call triggers eval.OppCribEV's one-time table build.
func OppCribRung() Discarder {
	return Discarder{
		Name: "L3-oppcrib",
		Discard: func(h [6]cribbage.Card, dealer bool, _, _ int) [2]cribbage.Card {
			sp := Splits(h, dealer)
			best := 0
			bestV := OppCribScore(sp[0], dealer)
			for i := 1; i < len(sp); i++ {
				if v := OppCribScore(sp[i], dealer); v > bestV {
					best, bestV = i, v
				}
			}
			return sp[best].Discard
		},
	}
}
