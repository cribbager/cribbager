package bot

import (
	"fmt"

	"github.com/cribbager/cribbager/internal/game"
)

// Result is the outcome of one bot-vs-bot game.
type Result struct {
	Winner game.Seat
	Scores [2]int
}

// PlayGame runs a complete game between two bots (a is seat 0, b is seat 1) over
// the given deck source and returns the result. An error means a bot produced an
// illegal move — which the engine rejects — so PlayGame doubles as the legality
// check.
func PlayGame(a, b Bot, deck game.DeckSource) (Result, error) {
	g := game.New(game.Options{Deck: deck})
	bots := [2]Bot{a, b}

	for {
		if w, ok := g.Winner(); ok {
			return Result{Winner: w, Scores: g.Scores()}, nil
		}
		v := g.View(game.Seat0)
		switch v.Phase {
		case game.PhaseDiscard:
			for s := game.Seat(0); s < 2; s++ {
				vs := g.View(s)
				if len(vs.YourHand) != 6 {
					continue
				}
				if _, err := g.Apply(s, game.Discard{Cards: bots[s].Discard(vs)}); err != nil {
					return Result{}, fmt.Errorf("%s discard: %w", bots[s].Name(), err)
				}
			}
		case game.PhasePlay:
			seat := *v.ToPlay
			vs := g.View(seat)
			if _, err := g.Apply(seat, game.Play{Card: bots[seat].Play(vs)}); err != nil {
				return Result{}, fmt.Errorf("%s play: %w", bots[seat].Name(), err)
			}
		default:
			return Result{}, fmt.Errorf("unexpected phase %v", v.Phase)
		}
	}
}
