package eval

import (
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// Opponent model for the play phase: how many points is the opponent likely to
// score in reply to a given pile/count? The opponent is modeled as holding
// oppHandSize cards drawn uniformly from the cards this seat cannot see, and
// playing their best legal reply — the exact E[max best reply] under the
// uniform-draw model, so every way a pile can be punished is priced in, weighted
// by how likely the opponent is to hold a card that does it.

// probHoldsNone is the probability that a draw of h cards from a pool of U
// contains NONE of k flagged cards — the hypergeometric "zero successes" tail,
// C(U-k, h) / C(U, h).
func probHoldsNone(U, k, h int) float64 {
	if k <= 0 {
		return 1
	}
	if U-k < h {
		return 0 // not enough non-flagged cards to fill the hand
	}
	p := 1.0
	for i := 0; i < h; i++ {
		p *= float64(U-k-i) / float64(U-i)
	}
	return p
}

// ExpectedOppReply is the expected points the opponent scores on their immediate
// reply to (pile, count) — where pile/count are AFTER this seat's play. It
// returns E[max over the cards the opponent actually holds], the exact expected
// best reply under the uniform-draw model.
func ExpectedOppReply(pile []cribbage.Card, count int, unseen []cribbage.Card, oppHandSize int) float64 {
	if oppHandSize <= 0 {
		return 0
	}
	U := len(unseen)
	if U == 0 {
		return 0
	}

	// Points each unseen card would score as the reply (0 if illegal at this count).
	pts := make([]int, len(unseen))
	for i, c := range unseen {
		if c.Rank.PipValue() <= 31-count {
			r, _ := pegging.Score(pile, c)
			pts[i] = r.Total
		}
	}

	// Distinct positive values, descending.
	seen := map[int]bool{}
	values := []int{}
	for _, p := range pts {
		if p > 0 && !seen[p] {
			seen[p] = true
			values = append(values, p)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))

	// E[max] = Σ_v  v · ( P(holds none scoring > v) − P(holds none scoring ≥ v) ).
	expected := 0.0
	countAtOrAbove := 0 // unseen cards scoring ≥ the current value
	for _, v := range values {
		above := countAtOrAbove // cards scoring strictly higher than v
		for _, p := range pts {
			if p == v {
				countAtOrAbove++
			}
		}
		pNoneAbove := probHoldsNone(U, above, oppHandSize)
		pNoneAtOrAbove := probHoldsNone(U, countAtOrAbove, oppHandSize)
		expected += float64(v) * (pNoneAbove - pNoneAtOrAbove)
	}
	return expected
}

// oppHandSize is how many cards the opponent has left to play. Both players keep
// the same number of cards after the discard, so it follows from what this seat
// holds and what each side has already laid down.
func oppHandSize(v game.PlayerView) int {
	return len(v.YourHand) + len(v.YourPlayed) - len(v.OpponentPlayed)
}

// pegTieBreak keeps low cards back for later go/31 reach — a tiny tie-breaker
// that only separates plays of equal net EV.
func pegTieBreak(c cribbage.Card) float64 { return 0.001 * float64(13-c.Rank.PipValue()) }

// RankedPlay is one legal play with its one-ply breakdown: the points it scores
// now, the expected opponent reply, and the net EV that the bot maximizes (the
// tie-breaker is folded into Score).
type RankedPlay struct {
	Card  cribbage.Card
	Mine  int     // immediate points
	Reply float64 // expected opponent reply
	Score float64 // net EV (Mine − Reply, plus the low-card tie-breaker)
}

// RankPlays scores every legal play best-first by one-ply net EV, pricing the
// opponent's reply with the calibrated belief (belief.go): unseen cards
// weighted by the champion's own discard policy, the player's remembered crib
// throw excluded, and exact elimination from a live-series go. Promoted over
// the uniform model at +0.66 pts/pair (95% CI [+0.44, +0.87], 6000 duplicate
// deal-pairs). It is the single source of truth for the play decision and its
// explanation; the belief is computed once per decision.
func RankPlays(v game.PlayerView) []RankedPlay {
	pool, q := calibratedBelief(v)
	return rankPlaysWith(v, func(pile []cribbage.Card, count int) float64 {
		return ExpectedOppReplyBelief(pile, count, pool, q)
	})
}

// rankPlaysWith is the shared ranking loop: score every legal play by immediate
// points minus the estimated opponent reply, best-first.
func rankPlaysWith(v game.PlayerView, reply func(pile []cribbage.Card, count int) float64) []RankedPlay {
	ranked := make([]RankedPlay, len(v.LegalPlays))
	for i, c := range v.LegalPlays {
		mine := PlayValue(v.Pile, c)
		count := v.Count + c.Rank.PipValue()
		rep := 0.0
		if count != 31 {
			newPile := append(append([]cribbage.Card(nil), v.Pile...), c)
			rep = reply(newPile, count)
		}
		ranked[i] = RankedPlay{
			Card:  c,
			Mine:  mine,
			Reply: rep,
			Score: float64(mine) - rep + pegTieBreak(c),
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	return ranked
}

// BestPlayNetEV picks the legal card with the best one-ply net EV (the canonical
// bot's whole play()).
func BestPlayNetEV(v game.PlayerView) cribbage.Card { return RankPlays(v)[0].Card }
