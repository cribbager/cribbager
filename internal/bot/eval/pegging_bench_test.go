package eval

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Bots decide under the server's session lock (driveBots), so a play decision
// must stay comfortably in the sub-millisecond range.
func benchView(b *testing.B) game.PlayerView {
	c := func(s string) cribbage.Card {
		cd, err := cribbage.ParseCard(s)
		if err != nil {
			b.Fatal(err)
		}
		return cd
	}
	starter := c("2D")
	return game.PlayerView{
		You:            game.Seat0,
		Dealer:         game.Seat1,
		Starter:        &starter,
		Pile:           []cribbage.Card{c("TH")},
		Count:          10,
		OpponentPlayed: []cribbage.Card{c("TH")},
		YourHand:       []cribbage.Card{c("5D"), c("9C"), c("2S"), c("KD")},
		YourDiscards:   []cribbage.Card{c("3H"), c("4H")},
		LegalPlays:     []cribbage.Card{c("5D"), c("9C"), c("2S"), c("KD")},
	}
}

func BenchmarkRankPlays(b *testing.B) {
	v := benchView(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RankPlays(v)
	}
}

// The win-objective discard runs the full deal walk for all 15 holds — the
// most expensive decision the bot makes, and only in reach of the target. It
// happens once per deal, so the budget is a few milliseconds.
func BenchmarkRankDiscardsWinEndgame(b *testing.B) {
	c := func(s string) cribbage.Card {
		cd, err := cribbage.ParseCard(s)
		if err != nil {
			b.Fatal(err)
		}
		return cd
	}
	h := [6]cribbage.Card{c("5H"), c("5C"), c("JD"), c("4S"), c("9C"), c("KD")}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RankDiscardsWin(h, true, 95, 100)
	}
}

func BenchmarkRankPlaysWinEndgame(b *testing.B) {
	v := benchView(b)
	v.Scores = [2]int{110, 112}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RankPlaysWin(v)
	}
}
