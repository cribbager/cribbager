package ladder

import (
	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// ladderBot pairs one rung's discard with the CHAMPION's pegging. Because both
// sides of a bot.Compare then peg identically on duplicate deals, the paired
// margin isolates DISCARD skill — the same trick lab/pegBot uses to isolate
// pegging. It embeds the champion for Version and any future Bot methods, and
// overrides only Name and Discard.
type ladderBot struct {
	bot.Bot // the champion: pegging (Play), Version
	d       Discarder
}

func (b ladderBot) Name() string { return b.d.Name }

func (b ladderBot) Discard(v game.PlayerView) [2]cribbage.Card {
	var h [6]cribbage.Card
	copy(h[:], v.YourHand)
	return b.d.Discard(h, v.Dealer == v.You, v.Scores[v.You], v.Scores[1-v.You])
}

// Bot wraps a rung as a full bot: that rung's discard, the champion's pegging.
func Bot(d Discarder) bot.Bot { return ladderBot{Bot: bot.Champion(), d: d} }

// Bots wraps the whole ladder floor-to-ceiling, sharing the seed with Ladder so
// L0's random discard is reproducible.
func Bots(seed int64) []bot.Bot {
	rungs := Ladder(seed)
	out := make([]bot.Bot, len(rungs))
	for i, r := range rungs {
		out[i] = Bot(r)
	}
	return out
}
