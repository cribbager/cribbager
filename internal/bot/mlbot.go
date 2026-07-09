package bot

import (
	"bytes"
	_ "embed"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/bot/peg"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/nn"
)

// mlPegWeights is the trained pegging Q-network (docs/research/ml-bot,
// chapters 4–5): 128→128→128→13, fitted-Q on pooled ε-greedy self-play.
// Embedded so the shipped bot has no runtime file dependency; regenerate via
// the training pipeline in ml/ and lab, and bump mlVersion when replacing.
//
//go:embed mlpeg-weights-v1.json
var mlPegWeights []byte

const mlVersion = "1"

// ml is the ML program's production bot — the first learned player to pass
// the promotion gates (Phase 3, docs/research/ml-bot chapter 6). It discards
// exactly like the champion (win-probability objective; the supervised
// discard experiment of chapters 2–3 showed imitation could only tie, so the
// exact evaluator stays) and pegs with the Q-network, which beat the
// champion's one-ply pegging by +0.70 pts/pair (95% CI +0.46..+0.95) on the
// pegging-isolated gate with no measurable endgame regression on the
// positional fixtures. Deterministic: same view in, same move out.
type ml struct {
	net *nn.MLP
}

// newML builds the production ML bot from the embedded weights. The weights
// are a build artifact — if they fail to parse, the binary is broken, so this
// panics rather than limping.
func newML() Bot {
	m, err := nn.Load(bytes.NewReader(mlPegWeights))
	if err != nil {
		panic("bot: embedded ml pegging weights: " + err.Error())
	}
	if m.InputSize() != peg.Dims || m.OutputSize() != peg.Actions {
		panic("bot: embedded ml pegging weights have the wrong shape")
	}
	return ml{net: m}
}

func (ml) Name() string    { return "ml" }
func (ml) Version() string { return mlVersion }

func (b ml) Discard(v game.PlayerView) [2]cribbage.Card {
	d, _ := eval.BestDiscardWin(hand6(v.YourHand), v.Dealer == v.You, v.Scores[v.You], v.Scores[1-v.You])
	return d
}

func (b ml) Play(v game.PlayerView) cribbage.Card {
	return peg.Net{M: b.net}.Play(v, nil) // greedy: the rng is never touched
}
