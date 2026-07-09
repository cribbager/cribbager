// Package peg is the pegging reinforcement-learning environment (chapter 4 of
// docs/research/ml-bot): a state encoder, pluggable play policies, and a
// self-play episode generator that turns the game engine's event log into
// Monte-Carlo training targets.
//
// Framing: pegging is a partially observable Markov decision process. The
// STATE is what one seat legitimately sees mid-play (its remaining cards, the
// series, the count, what has been played); the hidden part (the opponent's
// exact holding) is summarized by observable evidence (cards seen, cards
// remaining). The ACTION is a rank, not a card — suits never score during the
// play, so all suits of a rank are the same move and the action space is 13,
// not 52. The REWARD is pegging points, signed: +mine, −theirs. A decision's
// RETURN is the sum of rewards from that decision to the end of the deal's
// play (undiscounted — episodes are a handful of plies).
//
// Unlike the discard net (chapters 2–3), the encoder lives ONLY here in Go:
// the generator writes already-encoded vectors, training consumes them
// opaquely, and inference re-uses this same function — so there is no
// cross-language encoding to hold in parity, by construction.
package peg

import (
	"math"
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/nn"
)

// The encoding. Rank-centric throughout: pegging scoring (fifteens, pairs,
// runs, thirty-one) never reads suits.
const (
	Actions = 13 // one action per rank

	handOff   = 0   // 13: my remaining cards, count of each rank / 4
	seriesOff = 13  // 5×13: last 5 series cards, most recent first, rank one-hot
	countOff  = 78  // 32: the count, one-hot 0..31
	oppOff    = 110 // 5: opponent's remaining cards, one-hot 0..4
	seenOff   = 115 // 13: ranks unavailable to the opponent (played, starter,
	//       my hand, my crib throws), count of each / 4
	Dims = 128

	seriesSlots = 5
)

// Encode builds the network input from one seat's mid-play view.
func Encode(v game.PlayerView) []float64 {
	x := make([]float64, Dims)
	for _, c := range v.YourHand {
		x[handOff+int(c.Rank)-1] += 0.25
	}
	for s := 0; s < seriesSlots && s < len(v.Pile); s++ {
		c := v.Pile[len(v.Pile)-1-s]
		x[seriesOff+Actions*s+int(c.Rank)-1] = 1
	}
	if v.Count >= 0 && v.Count <= 31 {
		x[countOff+v.Count] = 1
	}
	opp := min(v.OpponentCards, 4)
	x[oppOff+opp] = 1
	seen := func(cs ...cribbage.Card) {
		for _, c := range cs {
			x[seenOff+int(c.Rank)-1] += 0.25
		}
	}
	seen(v.YourHand...)
	seen(v.YourPlayed...)
	seen(v.OpponentPlayed...)
	seen(v.YourDiscards...)
	if v.Starter != nil {
		seen(*v.Starter)
	}
	return x
}

// Policy chooses a pegging card from a view. The rng is for stochastic
// policies (exploration, tie-breaking); deterministic policies ignore it.
type Policy interface {
	Play(v game.PlayerView, rng *rand.Rand) cribbage.Card
}

// Random plays a uniformly random legal card — the floor of the pegging
// ladder and the maximum-exploration behavior policy for the first round of
// self-play data.
type Random struct{}

func (Random) Play(v game.PlayerView, rng *rand.Rand) cribbage.Card {
	return v.LegalPlays[rng.Intn(len(v.LegalPlays))]
}

// Champion pegs exactly like the shipped champion.
type Champion struct{}

func (Champion) Play(v game.PlayerView, _ *rand.Rand) cribbage.Card {
	return eval.BestPlayWin(v)
}

// Net pegs by a trained Q-network: Forward returns 13 values, one per rank —
// the predicted return of playing that rank here — and the policy takes the
// best legal one. With Epsilon > 0 it explores: that fraction of decisions is
// uniform random instead, so self-play data keeps covering actions the
// current net thinks are bad (it might be wrong — that's the point).
type Net struct {
	M       *nn.MLP
	Epsilon float64
}

func (p Net) Play(v game.PlayerView, rng *rand.Rand) cribbage.Card {
	if p.Epsilon > 0 && rng.Float64() < p.Epsilon {
		return v.LegalPlays[rng.Intn(len(v.LegalPlays))]
	}
	q := p.M.Forward(Encode(v))
	best, bq := v.LegalPlays[0], math.Inf(-1)
	for _, c := range v.LegalPlays {
		if qv := q[int(c.Rank)-1]; qv > bq {
			best, bq = c, qv
		}
	}
	return best
}
