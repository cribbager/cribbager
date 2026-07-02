package eval

import (
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// The win-probability discard objective. Far from the end, maximizing points
// maximizes wins and RankDiscards is authoritative; once either player is
// within reach of the target, the ORDER points land in (pegging, then pone
// hand, then dealer hand, then crib) and the SHAPE of each score distribution
// decide games, not the mean. Each hold is walked through the deal's scoring
// components in counting order, resolving a win or loss the moment a
// cumulative gain crosses a need, and holds are ranked by the resulting
// P(win).
//
// Distributions: this player's hand is exact (all 46 starters); the crib is
// exact under a uniform completion (cribDistTable); the opponent's hand and
// both players' pegging come from champion self-play marginals (winprob.go).
// Component independence is an accepted v1 approximation.

// cribScoreDist is the exact crib score distribution for throwing a and b,
// under a uniform completion of the other two cards and starter.
func cribScoreDist(a, b cribbage.Card) *[30]float32 {
	lo, hi := int(a.Rank), int(b.Rank)
	if lo > hi {
		lo, hi = hi, lo
	}
	suited := 0
	if a.Suit == b.Suit && lo != hi {
		suited = 1
	}
	return &cribDistTable[lo][hi][suited]
}

// dealWalk carries the joint (myGain, oppGain) probability grid through one
// scoring component at a time. Gains that cross a player's need resolve
// immediately into pWin/pLoss — whoever crosses first in counting order wins.
type dealWalk struct {
	needMe, needOpp int
	alive           []float64 // needMe×needOpp grid, index a*needOpp + b
	scratch         []float64
	pWin, pLoss     float64
}

func newDealWalk(needMe, needOpp int) *dealWalk {
	w := &dealWalk{
		needMe:  needMe,
		needOpp: needOpp,
		alive:   make([]float64, needMe*needOpp),
		scratch: make([]float64, needMe*needOpp),
	}
	w.alive[0] = 1
	return w
}

func (w *dealWalk) swap() {
	w.alive, w.scratch = w.scratch, w.alive
	for i := range w.scratch {
		w.scratch[i] = 0
	}
}

// applyMine convolves a score distribution earned by this player.
func (w *dealWalk) applyMine(dist []float64) {
	for a := 0; a < w.needMe; a++ {
		for b := 0; b < w.needOpp; b++ {
			p := w.alive[a*w.needOpp+b]
			if p == 0 {
				continue
			}
			for s, ps := range dist {
				if ps == 0 {
					continue
				}
				if a+s >= w.needMe {
					w.pWin += p * ps
				} else {
					w.scratch[(a+s)*w.needOpp+b] += p * ps
				}
			}
		}
	}
	w.swap()
}

// applyOpp convolves a score distribution earned by the opponent.
func (w *dealWalk) applyOpp(dist []float64) {
	for a := 0; a < w.needMe; a++ {
		for b := 0; b < w.needOpp; b++ {
			p := w.alive[a*w.needOpp+b]
			if p == 0 {
				continue
			}
			for s, ps := range dist {
				if ps == 0 {
					continue
				}
				if b+s >= w.needOpp {
					w.pLoss += p * ps
				} else {
					w.scratch[a*w.needOpp+(b+s)] += p * ps
				}
			}
		}
	}
	w.swap()
}

// applyPegging convolves the joint pegging distribution. Within a deal the
// pegging interleave is not modeled; the pone's pegging is counted before the
// dealer's (the pone leads) — an accepted approximation.
func (w *dealWalk) applyPegging(meDealer bool) {
	for a := 0; a < w.needMe; a++ {
		for b := 0; b < w.needOpp; b++ {
			p := w.alive[a*w.needOpp+b]
			if p == 0 {
				continue
			}
			for _, c := range pegJointDist {
				pw := p * float64(c.W)
				var myPts, oppPts int
				var poneIsMe bool
				if meDealer {
					myPts, oppPts, poneIsMe = c.D, c.P, false
				} else {
					myPts, oppPts, poneIsMe = c.P, c.D, true
				}
				// pone's points count first
				if poneIsMe {
					if a+myPts >= w.needMe {
						w.pWin += pw
						continue
					}
					if b+oppPts >= w.needOpp {
						w.pLoss += pw
						continue
					}
				} else {
					if b+oppPts >= w.needOpp {
						w.pLoss += pw
						continue
					}
					if a+myPts >= w.needMe {
						w.pWin += pw
						continue
					}
				}
				w.scratch[(a+myPts)*w.needOpp+(b+oppPts)] += pw
			}
		}
	}
	w.swap()
}

// applyHeels resolves his heels: 2 points to the dealer before anything else.
func (w *dealWalk) applyHeels(meDealer bool) {
	hp := float64(heelsProb)
	dist := []float64{1 - hp, 0, hp}
	if meDealer {
		w.applyMine(dist)
	} else {
		w.applyOpp(dist)
	}
}

// finish resolves the still-alive states into the next deal's win probability
// (the deal rotates: this player deals next iff they were pone).
func (w *dealWalk) finish(myScore, oppScore int, meDealer bool) float64 {
	p := w.pWin
	for a := 0; a < w.needMe; a++ {
		for b := 0; b < w.needOpp; b++ {
			if pa := w.alive[a*w.needOpp+b]; pa > 0 {
				p += pa * WinProb(myScore+a, oppScore+b, !meDealer)
			}
		}
	}
	return p
}

func dist32(d *[30]float32) []float64 {
	out := make([]float64, len(d))
	for i, v := range d {
		out[i] = float64(v)
	}
	return out
}

// holdWinProb walks one hold through the deal in counting order and returns
// P(win) from this position with that hold.
func holdWinProb(rd RankedDiscard, h [6]cribbage.Card, meDealer bool, myScore, oppScore int) float64 {
	w := newDealWalk(121-myScore, 121-oppScore)
	w.applyHeels(meDealer)
	w.applyPegging(meDealer)

	myHand := HandValueDist(rd.Keep, h[:])
	crib := dist32(cribScoreDist(rd.Discard[0], rd.Discard[1]))
	if meDealer {
		w.applyOpp(dist32(&oppHandDist[0])) // pone hand counts first
		w.applyMine(myHand[:])
		w.applyMine(crib)
	} else {
		w.applyMine(myHand[:])
		w.applyOpp(dist32(&oppHandDist[1]))
		w.applyOpp(crib)
	}
	return w.finish(myScore, oppScore, meDealer)
}

// RankDiscardsWin ranks the 15 holds by expected win probability, falling back
// to the points-EV order (Win fields zero) while nobody is in reach of the
// target — where the two objectives provably agree. RankDiscards keeps its
// points-EV semantics for the server's analysis and tools.
func RankDiscardsWin(h [6]cribbage.Card, myCrib bool, myScore, oppScore int) []RankedDiscard {
	ranked := RankDiscards(h, myCrib)
	if farFromEnd(myScore, oppScore, myCrib) {
		return ranked
	}
	for i := range ranked {
		ranked[i].Win = holdWinProb(ranked[i], h, myCrib, myScore, oppScore)
	}
	// Rank by win probability; points EV breaks near-ties so play stays sane
	// when the win landscape is locally flat.
	sort.SliceStable(ranked, func(i, j int) bool {
		if d := ranked[i].Win - ranked[j].Win; d > 1e-9 || d < -1e-9 {
			return d > 0
		}
		return ranked[i].Score > ranked[j].Score
	})
	return ranked
}

// BestDiscardWin is the win-objective counterpart of BestDiscardEV.
func BestDiscardWin(h [6]cribbage.Card, myCrib bool, myScore, oppScore int) (discard [2]cribbage.Card, keep [4]cribbage.Card) {
	top := RankDiscardsWin(h, myCrib, myScore, oppScore)[0]
	return top.Discard, top.Keep
}
