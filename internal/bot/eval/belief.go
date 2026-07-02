package eval

import (
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// The calibrated opponent model. Instead of drawing the opponent's hand
// uniformly from the unseen pool, it weights each unseen card by how likely a
// rational opponent is to be holding it:
//
//   - your own crib throw is excluded from the pool outright (you remember it,
//     and it cannot be in their hand);
//   - each remaining card is weighted by keepProb — the probability that the
//     champion's own discard policy keeps that rank (an unseen 5 is much more
//     likely in their hand than a uniform draw says, because nobody throws a 5
//     to the opponent's crib);
//   - a "go" is exact information: if the opponent passed in the live series,
//     every card at or below the pip they couldn't cover is impossible.

// calibratedBelief returns the pool of cards possibly in the opponent's hand
// and q[i] = P(pool[i] is in the opponent's remaining hand), an
// independent-inclusion approximation with Σq = the opponent's hand size.
func calibratedBelief(v game.PlayerView) (pool []cribbage.Card, q []float64) {
	pool = unseen(v)
	if len(v.YourDiscards) > 0 {
		mine := map[cribbage.Card]bool{}
		for _, c := range v.YourDiscards {
			mine[c] = true
		}
		kept := pool[:0]
		for _, c := range pool {
			if !mine[c] {
				kept = append(kept, c)
			}
		}
		pool = kept
	}

	role := 0 // opponent threw to the crib as pone…
	if v.Dealer != v.You {
		role = 1 // …or as dealer (their own crib)
	}
	w := make([]float64, len(pool))
	for i, c := range pool {
		w[i] = keepProb[role][c.Rank]
	}
	if maxPip, ok := oppPassConstraint(v); ok {
		for i, c := range pool {
			if c.Rank.PipValue() <= maxPip {
				w[i] = 0
			}
		}
	}
	return pool, scaleToHandSize(w, oppHandSize(v))
}

// oppPassConstraint reports the exact inference from an opponent "go" in the
// live series: if they passed, they hold no card with pip value ≤ maxPip.
// Reconstructed from the view alone — every pile card belongs to one seat's
// played list, and when it is my turn with the tail of the pile mine, the
// opponent must have passed right after my first trailing card.
func oppPassConstraint(v game.PlayerView) (maxPip int, ok bool) {
	if len(v.Pile) == 0 {
		return 0, false
	}
	mine := map[cribbage.Card]bool{}
	for _, c := range v.YourPlayed {
		mine[c] = true
	}
	// First index where the trailing run of my cards begins.
	start := len(v.Pile)
	for start > 0 && mine[v.Pile[start-1]] {
		start--
	}
	if start == len(v.Pile) {
		return 0, false // pile ends with their card: no pass evidence
	}
	// They passed at the count right after my first trailing card.
	count := 0
	for _, c := range v.Pile[:start+1] {
		count += c.Rank.PipValue()
	}
	return 31 - count, true
}

// scaleToHandSize scales weights into inclusion probabilities q ≤ 1 with
// Σq = h (water-filling: cards whose scaled weight exceeds 1 are clamped and
// the excess redistributed). Zero-weight cards stay at zero — those exclusions
// are exact — so Σq can fall short of h only in degenerate hand-built views.
func scaleToHandSize(w []float64, h int) []float64 {
	q := make([]float64, len(w))
	if h <= 0 {
		return q
	}
	clamped := make([]bool, len(w))
	remaining := float64(h)
	for iter := 0; iter <= len(w); iter++ {
		var s float64
		for i := range w {
			if !clamped[i] {
				s += w[i]
			}
		}
		if s <= 0 {
			return q
		}
		f := remaining / s
		progressed := false
		for i := range w {
			if !clamped[i] && f*w[i] >= 1 {
				clamped[i] = true
				q[i] = 1
				remaining--
				progressed = true
			}
		}
		if !progressed {
			for i := range w {
				if !clamped[i] {
					q[i] = f * w[i]
				}
			}
			return q
		}
	}
	return q
}

// ExpectedOppReplyBelief is E[max reply] under per-card inclusion probabilities
// q — the belief-model counterpart of ExpectedOppReply. Independent inclusion
// makes P(holds none of set S) = Π_{i∈S} (1−q_i), and the telescoping over
// distinct reply values is otherwise identical.
func ExpectedOppReplyBelief(pile []cribbage.Card, count int, pool []cribbage.Card, q []float64) float64 {
	if len(pool) == 0 {
		return 0
	}

	// Group Π(1−q_i) by the points each card would score as the reply.
	prodByPts := map[int]float64{}
	for i, c := range pool {
		if c.Rank.PipValue() > 31-count || q[i] <= 0 {
			continue
		}
		r, _ := pegging.Score(pile, c)
		if r.Total <= 0 {
			continue
		}
		p, seen := prodByPts[r.Total]
		if !seen {
			p = 1
		}
		prodByPts[r.Total] = p * (1 - q[i])
	}

	values := make([]int, 0, len(prodByPts))
	for v := range prodByPts {
		values = append(values, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))

	// E[max] = Σ_v v · ( P(none scoring > v) − P(none scoring ≥ v) ).
	expected := 0.0
	prodAbove := 1.0
	for _, v := range values {
		prodAtOrAbove := prodAbove * prodByPts[v]
		expected += float64(v) * (prodAbove - prodAtOrAbove)
		prodAbove = prodAtOrAbove
	}
	return expected
}
