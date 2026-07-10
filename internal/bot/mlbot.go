package bot

import (
	"bytes"
	_ "embed"
	"math"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/bot/peg"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/nn"
)

// The ml bot's trained networks, embedded so the shipped bot has no runtime
// file dependency. Weights are build artifacts — regenerate via the training
// pipelines in ml/ and the lab, and bump mlVersion when replacing either.
//
// mlPegWeights (chapters 4–5): pegging Q-network, 128→128→128→13, fitted-Q
// on pooled ε-greedy self-play.
//
// mlDiscardWeights (chapter 7): discard pegging-differential net, 105→…→1 —
// predicts a keep's realized pegging differential under this bot's own
// pegging, the one deal component the exact evaluator cannot rank per hold.
//
//go:embed mlpeg-weights-v1.json
var mlPegWeights []byte

//go:embed mldiscard-weights-v1.json
var mlDiscardWeights []byte

// mlVersion history:
// v1: champion discards + Q-network pegging — promoted at +0.70 pts/pair
// (CI +0.46..+0.95) on the pegging-isolated gate, fixtures clean.
// v2: discards by exact expected points PLUS the learned pegging
// differential of the keep (residual learning, chapter 7) — promoted at
// +0.78 pts/pair (CI +0.55..+1.02) over v1 on the discard-isolated gate at
// win-parity, and +0.008 wins/pair (CI +0.002..+0.013) pooled over the
// positional fixtures (the pegging-aware keeps WIN endgames: +0.043 at
// 118-118, where pegging is the whole game).
const mlVersion = "2"

// ml is the ML program's production bot — the learned player that passed the
// promotion gates (docs/research/ml-bot). Both decisions are deterministic:
// same view in, same move out.
type ml struct {
	pegNet     *nn.MLP
	discardNet *nn.MLP
}

// newML builds the production ML bot from the embedded weights. The weights
// are build artifacts — if they fail to parse, the binary is broken, so this
// panics rather than limping.
func newML() Bot {
	pn, err := nn.Load(bytes.NewReader(mlPegWeights))
	if err != nil {
		panic("bot: embedded ml pegging weights: " + err.Error())
	}
	if pn.InputSize() != peg.Dims || pn.OutputSize() != peg.Actions {
		panic("bot: embedded ml pegging weights have the wrong shape")
	}
	dn, err := nn.Load(bytes.NewReader(mlDiscardWeights))
	if err != nil {
		panic("bot: embedded ml discard weights: " + err.Error())
	}
	if dn.InputSize() != discardInputDim || dn.OutputSize() != 1 {
		panic("bot: embedded ml discard weights have the wrong shape")
	}
	return ml{pegNet: pn, discardNet: dn}
}

func (ml) Name() string    { return "ml" }
func (ml) Version() string { return mlVersion }

// Discard ranks all 15 splits by exact expected points (hand + signed crib)
// plus the net's predicted pegging differential for the keep, and throws the
// argmax. Deliberately score-blind: the positional fixtures measured this
// AHEAD of the win-probability discard on endgame wins — pegging-aware keeps
// are worth more than the win-walk's hold-independent pegging approximation.
func (b ml) Discard(v game.PlayerView) [2]cribbage.Card {
	h := v.YourHand
	dealer := v.Dealer == v.You
	best, pick := math.Inf(-1), [2]cribbage.Card{}
	for _, rd := range eval.RankDiscards(hand6(h), dealer) {
		cards := append(append([]cribbage.Card{}, rd.Keep[:]...), rd.Discard[:]...)
		val := rd.Score + b.discardNet.Forward(DiscardInput(cards, 4, 5, dealer))[0]
		if val > best {
			best, pick = val, rd.Discard
		}
	}
	return pick
}

func (b ml) Play(v game.PlayerView) cribbage.Card {
	return peg.Net{M: b.pegNet}.Play(v, nil) // greedy: the rng is never touched
}

// The discard nets' input encoding: multi-hot keep, multi-hot discard, dealer
// flag. Mirrored EXACTLY by ml/cribml/data.py and held equal by the lab's
// Python-generated parity fixture (lab/mlbot_test.go). Card index is
// 4*(rank-1) + suit, matching internal/cribbage's Rank/Suit values.
const (
	discardInputDim  = 105 // 52 keep + 52 discard + dealer flag
	discardDealerBit = 104
)

// DiscardInput builds the discard-net input for throwing hand[di] and
// hand[dj] from a six-card hand.
func DiscardInput(hand []cribbage.Card, di, dj int, dealer bool) []float64 {
	x := make([]float64, discardInputDim)
	for k, c := range hand {
		idx := 4*(int(c.Rank)-1) + int(c.Suit)
		if k == di || k == dj {
			x[52+idx] = 1
		} else {
			x[idx] = 1
		}
	}
	if dealer {
		x[discardDealerBit] = 1
	}
	return x
}

// The win-target discard encoding (docs/research/ml-bot chapter 8): the base
// split encoding plus the game position and the split's exact expected
// points. Scores enter both as scalars and as coarse one-hot buckets so the
// net can carve the win surface into regions (par-hole curvature) without
// having to bend scalars alone; the exact Score feature means the net never
// spends capacity rediscovering point values (chapter 7's lesson as feature
// engineering). Encoded ONLY here in Go — the generator emits ready vectors,
// training consumes them opaquely, inference reuses this function.
const (
	winDiscardDims    = 128
	winScoreScalarOff = 105 // my/121, opp/121
	winMyBucketOff    = 107 // 10 buckets of my score
	winOppBucketOff   = 117 // 10 buckets of opponent score
	winSplitScoreOff  = 127 // the split's exact expected points / 30
)

// DiscardInputWin builds the position-aware discard input: hand/split as in
// DiscardInput, plus the decider's and opponent's scores and the split's
// exact expected points (eval.RankDiscards Score).
func DiscardInputWin(hand []cribbage.Card, di, dj int, dealer bool, my, opp int, score float64) []float64 {
	x := append(DiscardInput(hand, di, dj, dealer), make([]float64, winDiscardDims-discardInputDim)...)
	x[winScoreScalarOff] = float64(my) / 121
	x[winScoreScalarOff+1] = float64(opp) / 121
	x[winMyBucketOff+min(my*10/121, 9)] = 1
	x[winOppBucketOff+min(opp*10/121, 9)] = 1
	x[winSplitScoreOff] = score / 30
	return x
}
