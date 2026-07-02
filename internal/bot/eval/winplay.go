package eval

import (
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// The win-probability play objective. Far from the end it defers to RankPlays
// (net point EV, where the objectives agree); in reach of the target it scores
// each play by P(win): my points land first (pegging out wins immediately),
// then the opponent's best reply — priced by the calibrated belief — lands on
// theirs, and the still-undecided mass continues at the table's win
// probability for the updated scores. Mid-deal states reuse the deal-start
// table, an accepted approximation.

// replyProb is one point of the opponent-reply distribution.
type replyProb struct {
	Val int
	P   float64
}

// oppReplyDist is the distribution of the opponent's best immediate reply
// value under the belief, including 0 (no score or no legal reply), in
// descending value order (a slice, not a map, so downstream float accumulation
// is deterministic). The telescoping products of ExpectedOppReplyBelief, kept
// as P(max = v) instead of collapsed to the mean.
func oppReplyDist(pile []cribbage.Card, count int, pool []cribbage.Card, q []float64) []replyProb {
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

	dist := make([]replyProb, 0, len(values)+1)
	prodAbove := 1.0
	for _, v := range values {
		prodAtOrAbove := prodAbove * prodByPts[v]
		dist = append(dist, replyProb{Val: v, P: prodAbove - prodAtOrAbove})
		prodAbove = prodAtOrAbove
	}
	dist = append(dist, replyProb{Val: 0, P: prodAbove}) // no scoring reply
	return dist
}

// RankPlaysWin ranks legal plays by P(win). RankPlays keeps its net-EV
// semantics for explanations and far-from-end play.
func RankPlaysWin(v game.PlayerView) []RankedPlay {
	my, opp := v.Scores[v.You], v.Scores[1-v.You]
	meDealer := v.Dealer == v.You
	if farFromEnd(my, opp, meDealer) {
		return RankPlays(v)
	}

	pool, q := calibratedBelief(v)
	ranked := make([]RankedPlay, len(v.LegalPlays))
	for i, c := range v.LegalPlays {
		mine := PlayValue(v.Pile, c)
		count := v.Count + c.Rank.PipValue()
		my2 := my + mine

		var win, reply float64
		switch {
		case my2 >= 121:
			win = 1 // pegging out ends the game before any reply
		case count == 31:
			win = WinProb(my2, opp, meDealer)
		default:
			newPile := append(append([]cribbage.Card(nil), v.Pile...), c)
			for _, rp := range oppReplyDist(newPile, count, pool, q) {
				reply += float64(rp.Val) * rp.P
				if opp+rp.Val >= 121 {
					continue // they count out: contributes 0 wins
				}
				win += rp.P * WinProb(my2, opp+rp.Val, meDealer)
			}
		}
		ranked[i] = RankedPlay{
			Card:  c,
			Mine:  mine,
			Reply: reply,
			// Win decides; the net-EV term only separates plays whose win
			// probabilities are indistinguishable.
			Score: win + 1e-6*(float64(mine)-reply+pegTieBreak(c)),
			Win:   win,
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	return ranked
}

// BestPlayWin is the win-objective counterpart of BestPlayNetEV.
func BestPlayWin(v game.PlayerView) cribbage.Card { return RankPlaysWin(v)[0].Card }
