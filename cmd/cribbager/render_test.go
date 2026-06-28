package main

import (
	"strings"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestRendering drives a real hand through the engine and renders the table and
// the count screen, asserting the output is well-formed. It exercises the
// rendering pipeline without interactive input.
func TestRendering(t *testing.T) {
	g := game.New(game.Options{Deck: game.NewSeededDeck(7)})
	u := &ui{g: g, you: game.Seat0, opp: game.Seat1}

	// Both seats discard their first two cards.
	for s := game.Seat(0); s < 2; s++ {
		v := g.View(s)
		if _, err := g.Apply(s, game.Discard{Cards: [2]cribbage.Card{v.YourHand[0], v.YourHand[1]}}); err != nil {
			t.Fatalf("discard: %v", err)
		}
	}
	for _, e := range g.Events() {
		if sc, ok := e.(game.StarterCut); ok {
			u.starter = sc.Card.String()
		}
	}

	table := u.renderTable(g.View(u.you))
	for _, want := range []string{"♠ ♥ Cribbage", "┌─────┐", "╭", "You 0"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table missing %q:\n%s", want, table)
		}
	}

	// Play the hand out (legal moves) until the show.
	dealer := g.View(u.you).Dealer
	var show []game.Event
	for i := 0; i < 16 && len(show) == 0; i++ {
		pv := g.View(game.Seat0)
		if pv.ToPlay == nil {
			break
		}
		seat := *pv.ToPlay
		legal := g.View(seat).LegalPlays
		if len(legal) == 0 {
			t.Fatalf("seat %v has the turn but no legal play", seat)
		}
		evs, err := g.Apply(seat, game.Play{Card: legal[0]})
		if err != nil {
			t.Fatalf("play: %v", err)
		}
		for _, e := range evs {
			switch e.(type) {
			case game.HandShown, game.CribShown:
				show = append(show, e)
			}
		}
	}
	if len(show) == 0 {
		t.Fatal("no show events produced")
	}

	count := u.renderCount(show, u.starter, dealer)
	if !strings.Contains(count, "pts") {
		t.Fatalf("count screen missing scores:\n%s", count)
	}
}
