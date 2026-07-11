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
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
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
	EHand   float64 // L2: exact expected hand value over all 46 starters
	Crib    float64 // signed uniform-opponent crib EV of the discard (+dealer, −pone)
	Score   float64 // L3: EHand + Crib
}

// Splits enumerates all 15 holds in the canonical discardPairs order (i<j over
// the six cards), computing each rung's objective. The expensive term
// (ExpectedHandValue, a 46-starter sweep) is computed once here and shared by
// L2 and L3, so the harness pays it a single time per hand.
func Splits(h [6]cribbage.Card, dealer bool) []Split {
	sign := -1.0
	if dealer {
		sign = 1.0
	}
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
			eHand := eval.ExpectedHandValue(keep, h[:])
			crib := sign * eval.CribEV(h[i], h[j])
			out = append(out, Split{
				Discard: [2]cribbage.Card{h[i], h[j]},
				Keep:    keep,
				Show:    ShowValueNoStarter(keep),
				EHand:   eHand,
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
