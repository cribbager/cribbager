package lab

import (
	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// candidate is the starting template for a challenger. It embeds the shipped
// champion, so out of the box it plays IDENTICALLY — a no-op challenger that
// ties the champion in duplicate deals (a sanity check that the harness works).
//
// To run an experiment:
//  1. copy this file, rename the type and the Register name, and
//  2. override Discard and/or Play below with the idea you want to test.
//
// Then evaluate it against the champion:
//
//	CHALLENGE=<name> go test ./internal/bot/lab -run ChallengerVsChampion -v
//
// If it wins by a margin whose 95% CI clears zero, fold the change into the
// champion (internal/bot) and delete the challenger. If not, delete it anyway.
type candidate struct{ bot.Bot } // embeds the champion

func init() {
	Register("candidate", func() bot.Bot { return candidate{bot.Champion()} })
}

func (candidate) Name() string { return "candidate" }

// Discard and Play currently delegate to the embedded champion. Replace a body
// with your experimental logic to make this challenger differ from the champion.
func (c candidate) Discard(v game.PlayerView) [2]cribbage.Card { return c.Bot.Discard(v) }
func (c candidate) Play(v game.PlayerView) cribbage.Card       { return c.Bot.Play(v) }
