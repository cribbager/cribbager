package lab

import (
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/lab/peg"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/nn"
)

// pegBot pairs the champion's discards with an experimental pegging policy —
// the pegging-isolated instrument of the ML program (docs/research/ml-bot
// chapter 4). Because both sides of a compare then discard identically on
// duplicate deals, the paired margin measures pegging skill alone.
type pegBot struct {
	bot.Bot // the champion: discards (and any future Bot methods)
	play    peg.Policy
	rng     *rand.Rand
	name    string
	version string
}

func (b *pegBot) Name() string    { return b.name }
func (b *pegBot) Version() string { return b.version }
func (b *pegBot) Play(v game.PlayerView) cribbage.Card {
	return b.play.Play(v, b.rng)
}

func init() {
	// peg-random is the floor: champion discards, uniformly random pegging.
	// Its gate margin against the champion measures the total room between
	// the worst and the current pegging — the gap learning has to close.
	Register("peg-random", func() bot.Bot {
		return &pegBot{
			Bot: bot.Champion(), play: peg.Random{},
			rng: rand.New(rand.NewSource(2)), name: "peg-random", version: "1",
		}
	})
	// ml-peg is the learner: champion discards, Q-network pegging (greedy).
	// Retrain and reinstall weights with ml/scripts/train_pegging.py.
	Register("ml-peg", func() bot.Bot {
		m, err := nn.LoadFile("testdata/pegging-v1.json")
		if err != nil {
			panic("lab: ml-peg: " + err.Error())
		}
		return &pegBot{
			Bot: bot.Champion(), play: peg.Net{M: m},
			rng: rand.New(rand.NewSource(3)), name: "ml-peg", version: "1",
		}
	})
}
