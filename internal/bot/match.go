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
	return playOn(game.New(game.Options{Deck: deck}), [2]Bot{a, b})
}

// PlayGameFrom is PlayGame starting from a preset position (scores and dealer) —
// the runner behind positional fixtures, where score-aware play concentrates.
func PlayGameFrom(a, b Bot, deck game.DeckSource, start game.Start) (Result, error) {
	return playOn(game.New(game.Options{Deck: deck, Start: &start}), [2]Bot{a, b})
}

// PlayGameEvents is PlayGame but also returns the finished game's event log, for
// per-deal statistics (DealStats) and outcome-distribution collection.
func PlayGameEvents(a, b Bot, deck game.DeckSource) (Result, []game.Event, error) {
	g := game.New(game.Options{Deck: deck})
	r, err := playOn(g, [2]Bot{a, b})
	if err != nil {
		return Result{}, nil, err
	}
	return r, g.Events(), nil
}

// playOn drives an in-progress game to completion with bots[s] choosing for seat s.
func playOn(g *game.Game, bots [2]Bot) (Result, error) {
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
