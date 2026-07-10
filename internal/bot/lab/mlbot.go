package lab

import (
	"fmt"
	"math"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/nn"
)

// mlDiscard is the ML program's first bot on the board (chapter 3 of
// docs/research/ml-bot): it discards with the supervised value network from
// chapter 2 — fifteen forward passes, take the argmax — and pegs with the
// champion's play. Its job is to prove the train-in-Python/infer-in-Go chain
// end to end, not to win: the net's teacher was the champion's own point-EV
// evaluator, so by construction it can at best tie on points, and it is
// score-blind at the discard, so near the target it gives up the champion's
// win-probability edge. The interesting result is HOW LITTLE it loses by.
type mlDiscard struct {
	bot.Bot // the champion: pegging, and any future Bot methods
	net     *nn.MLP
}

func init() {
	Register("ml-discard", func() bot.Bot { return newMLDiscard("testdata/discard-v1.json") })
}

// newMLDiscard loads the trained weights (regenerate with
// ml/scripts/make_bot_parity.py after retraining). Lab bots are built only
// from tests, so a missing or malformed weights file panics rather than
// returning an error nothing would handle.
func newMLDiscard(weights string) bot.Bot {
	net, err := nn.LoadFile(weights)
	if err != nil {
		panic("lab: ml-discard: " + err.Error())
	}
	if net.InputSize() != inputDim || net.OutputSize() != 1 {
		panic(fmt.Sprintf("lab: ml-discard: model is %d->%d, want %d->1",
			net.InputSize(), net.OutputSize(), inputDim))
	}
	return &mlDiscard{Bot: bot.Champion(), net: net}
}

func (m *mlDiscard) Name() string    { return "ml-discard" }
func (m *mlDiscard) Version() string { return "1" }

// The input encoding lives in production (bot.DiscardInput, mlbot.go there):
// it mirrors ml/cribml/data.py EXACTLY, and the two are held equal by
// TestMLDiscardParity against a Python-generated fixture. Any change to
// either side must regenerate the fixture and keep that test green.
const inputDim = 105 // 52 keep multi-hot + 52 discard multi-hot + dealer flag

// Discard scores all 15 splits with the network and throws the argmax.
func (m *mlDiscard) Discard(v game.PlayerView) [2]cribbage.Card {
	h := v.YourHand
	if len(h) != 6 {
		panic(fmt.Sprintf("lab: ml-discard: Discard called with %d cards", len(h)))
	}
	dealer := v.Dealer == v.You
	best, pick := math.Inf(-1), [2]cribbage.Card{}
	for i := 0; i < len(h)-1; i++ {
		for j := i + 1; j < len(h); j++ {
			if val := m.net.Forward(bot.DiscardInput(h, i, j, dealer))[0]; val > best {
				best, pick = val, [2]cribbage.Card{h[i], h[j]}
			}
		}
	}
	return pick
}
