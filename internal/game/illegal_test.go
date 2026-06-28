package game

import (
	"errors"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

func mustCard(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatalf("bad card %q: %v", s, err)
	}
	return c
}

// deckWith returns a full 52-card deck whose first cards are the ones named,
// followed by the rest of the deck in order. Lets a test fix the deal exactly.
func deckWith(t *testing.T, names ...string) []cribbage.Card {
	t.Helper()
	var out []cribbage.Card
	seen := map[cribbage.Card]bool{}
	for _, n := range names {
		c := mustCard(t, n)
		out = append(out, c)
		seen[c] = true
	}
	for _, c := range cribbage.Deck() {
		if !seen[c] {
			out = append(out, c)
		}
	}
	return out
}

// scriptedGame deals a known hand: dealer is Seat1, so Seat0 (pone) leads.
// Seat0 holds TH 2D KH AH (+ 5C 5D to discard); Seat1 holds TC 3H 7C 8C
// (+ 6D 6H to discard); the starter is 9S.
func scriptedGame(t *testing.T) *Game {
	cut := deckWith(t, "KS", "2C") // KS > 2C, so Seat1 deals
	deal := deckWith(t,
		"TH", "TC", "2D", "3H", "KH", "7C", "AH", "8C", "5C", "6D", "5D", "6H", "9S")
	return New(Options{Deck: NewScriptedDeck(cut, deal)})
}

func TestIllegalDiscards(t *testing.T) {
	g := scriptedGame(t)
	if g.dealer != Seat1 {
		t.Fatalf("dealer = %v, want Seat1", g.dealer)
	}

	// Card not in hand.
	if _, err := g.Apply(Seat0, Discard{Cards: [2]cribbage.Card{mustCard(t, "QS"), mustCard(t, "5C")}}); !errors.Is(err, ErrNotInHand) {
		t.Errorf("not-in-hand discard: err = %v, want ErrNotInHand", err)
	}
	// Same card twice.
	if _, err := g.Apply(Seat0, Discard{Cards: [2]cribbage.Card{mustCard(t, "5C"), mustCard(t, "5C")}}); !errors.Is(err, ErrDuplicateDiscard) {
		t.Errorf("duplicate discard: err = %v, want ErrDuplicateDiscard", err)
	}
	// Playing during the discard phase.
	if _, err := g.Apply(Seat0, Play{Card: mustCard(t, "TH")}); !errors.Is(err, ErrWrongPhase) {
		t.Errorf("play in discard phase: err = %v, want ErrWrongPhase", err)
	}

	// Valid discard, then discarding again.
	if _, err := g.Apply(Seat0, Discard{Cards: [2]cribbage.Card{mustCard(t, "5C"), mustCard(t, "5D")}}); err != nil {
		t.Fatalf("valid discard failed: %v", err)
	}
	if _, err := g.Apply(Seat0, Discard{Cards: [2]cribbage.Card{mustCard(t, "TH"), mustCard(t, "2D")}}); !errors.Is(err, ErrAlreadyDiscarded) {
		t.Errorf("second discard: err = %v, want ErrAlreadyDiscarded", err)
	}
}

func TestIllegalPlays(t *testing.T) {
	g := scriptedGame(t)
	must := func(seat Seat, cmd Command) {
		t.Helper()
		if _, err := g.Apply(seat, cmd); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	play := func(seat Seat, card string) { must(seat, Play{Card: mustCard(t, card)}) }

	must(Seat0, Discard{Cards: [2]cribbage.Card{mustCard(t, "5C"), mustCard(t, "5D")}})
	must(Seat1, Discard{Cards: [2]cribbage.Card{mustCard(t, "6D"), mustCard(t, "6H")}})
	// now in the play phase; Seat0 leads
	if g.phase != PhasePlay {
		t.Fatalf("phase = %v, want play", g.phase)
	}

	play(Seat0, "TH") // 10
	play(Seat1, "TC") // 20
	play(Seat0, "2D") // 22
	play(Seat1, "3H") // 25 — Seat0 to act, holding KH and AH

	// Not your turn.
	if _, err := g.Apply(Seat1, Play{Card: mustCard(t, "7C")}); !errors.Is(err, ErrNotYourTurn) {
		t.Errorf("out-of-turn play: err = %v, want ErrNotYourTurn", err)
	}
	// Card not in hand.
	if _, err := g.Apply(Seat0, Play{Card: mustCard(t, "TC")}); !errors.Is(err, ErrNotInHand) {
		t.Errorf("not-in-hand play: err = %v, want ErrNotInHand", err)
	}
	// Would exceed 31 (25 + KH=10 = 35), while AH is legal.
	if _, err := g.Apply(Seat0, Play{Card: mustCard(t, "KH")}); !errors.Is(err, ErrCountExceeds31) {
		t.Errorf("over-31 play: err = %v, want ErrCountExceeds31", err)
	}
}

func TestApplyAfterGameOver(t *testing.T) {
	g := playFullGame(t, 1, false)
	if _, ok := g.Winner(); !ok {
		t.Fatal("expected a finished game")
	}
	if _, err := g.Apply(Seat0, Discard{}); !errors.Is(err, ErrGameOver) {
		t.Errorf("apply after game over: err = %v, want ErrGameOver", err)
	}
}
