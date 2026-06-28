package game

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// discard builds a Discard command from two card names.
func discard(t *testing.T, a, b string) Discard {
	t.Helper()
	return Discard{Cards: [2]cribbage.Card{mustCard(t, a), mustCard(t, b)}}
}

// hasEvent reports whether any event in the log satisfies pred.
func hasEvent(events []Event, pred func(Event) bool) bool {
	for _, e := range events {
		if pred(e) {
			return true
		}
	}
	return false
}

// TestHisHeelsWinsTheGame pins the his-heels win path: cutting a Jack starter
// awards 2 to the dealer, and with a target of 2 that ends the game at the cut —
// before any pegging or show. If checkWin were not called after the heels award
// (game.go cutStarter), the game would continue past the cut and this fails.
func TestHisHeelsWinsTheGame(t *testing.T) {
	// 2C < KS, so Seat0 deals (dealer=Seat0, pone=Seat1). The pone gets the even
	// deal indices, the dealer the odd ones, and index 12 is the starter — a Jack.
	cut := deckWith(t, "2C", "KS")
	deal := deckWith(t,
		"3H", "4C", "5H", "6C", "7H", "8C",
		"3D", "4D", "5D", "6D", "7D", "8D",
		"JH") // starter is a Jack -> his heels
	g := New(Options{Deck: NewScriptedDeck(cut, deal), TargetScore: 2})

	if g.dealer != Seat0 {
		t.Fatalf("dealer = %v, want Seat0", g.dealer)
	}
	dealer := g.dealer
	pone := other(dealer)

	// Both seats discard two legal cards; the second discard triggers the cut.
	// pone (Seat1) holds 3H 5H 7H 3D 5D 7D; dealer (Seat0) holds 4C 6C 8C 4D 6D 8D.
	if _, err := g.Apply(pone, discard(t, "7H", "7D")); err != nil {
		t.Fatalf("pone discard: %v", err)
	}
	evs, err := g.Apply(dealer, discard(t, "8C", "8D"))
	if err != nil {
		t.Fatalf("dealer discard: %v", err)
	}

	// The cut produced a StarterCut with Heels == 2.
	var sawHeels bool
	for _, e := range evs {
		if sc, ok := e.(StarterCut); ok {
			if sc.Heels != 2 {
				t.Fatalf("StarterCut.Heels = %d, want 2", sc.Heels)
			}
			if sc.Card.Rank != cribbage.Jack {
				t.Fatalf("starter = %v, want a Jack", sc.Card)
			}
			sawHeels = true
		}
	}
	if !sawHeels {
		t.Fatal("no StarterCut event with heels")
	}

	// The dealer reached the target on the heels and won at the cut.
	if got := g.Scores()[dealer]; got != 2 {
		t.Fatalf("dealer score = %d, want 2", got)
	}
	if w, ok := g.Winner(); !ok || w != dealer {
		t.Fatalf("winner = (%v,%v), want (%v,true)", w, ok, dealer)
	}

	// The game ended at the cut: no pegging or show occurred.
	all := g.Events()
	if hasEvent(all, func(e Event) bool { _, ok := e.(CardPlayed); return ok }) {
		t.Error("a card was played; the game should have ended at the cut")
	}
	if hasEvent(all, func(e Event) bool { _, ok := e.(HandShown); return ok }) {
		t.Error("a hand was shown; the game should have ended at the cut")
	}
	if hasEvent(all, func(e Event) bool { _, ok := e.(CribShown); return ok }) {
		t.Error("the crib was shown; the game should have ended at the cut")
	}
}

// TestPoneWinsAtShowBeforeDealerCounts pins the show ordering in resolveShow:
// the pone's hand is counted first, and the moment it crosses the target the
// game ends — the dealer's hand and the crib are never scored. If the dealer or
// crib were counted before checking the pone's win, the dealer (whose hand and
// crib both also reach the target here) would win instead, failing this test.
func TestPoneWinsAtShowBeforeDealerCounts(t *testing.T) {
	// 2C < KS, so Seat0 deals (dealer=Seat0, pone=Seat1). Pone holds the even deal
	// indices, dealer the odd ones, starter (index 12) is 9S (not a Jack).
	//
	// Pone keep:   KH KD KS TH  (discards 2H, 2D)
	// Dealer keep: KC TC TD TS  (discards 2C, 2S)
	// Crib:        2H 2D 2C 2S  + starter 9S
	cut := deckWith(t, "2C", "KS")
	deal := deckWith(t,
		"KH", "KC", "KD", "TC", "KS", "TD",
		"TH", "TS", "2H", "2C", "2D", "2S",
		"9S")
	g := New(Options{Deck: NewScriptedDeck(cut, deal), TargetScore: 5})

	if g.dealer != Seat0 {
		t.Fatalf("dealer = %v, want Seat0", g.dealer)
	}
	dealer, pone := Seat0, Seat1

	apply := func(seat Seat, cmd Command) {
		t.Helper()
		if _, err := g.Apply(seat, cmd); err != nil {
			t.Fatalf("seat %v %T: %v", seat, cmd, err)
		}
	}
	play := func(seat Seat, c string) { apply(seat, Play{Card: mustCard(t, c)}) }

	apply(pone, discard(t, "2H", "2D"))
	apply(dealer, discard(t, "2C", "2S"))

	// Drive the play. All cards are ten-valued and ordered so no fifteen, pair, or
	// run ever forms — only go / last-card points accrue. Pone (Seat1) leads.
	play(pone, "KH")   // 10
	play(dealer, "TC") // 20
	play(pone, "KD")   // 30  -> go to pone (Seat1)
	play(dealer, "TD") // 10  (new series, dealer leads)
	play(pone, "KS")   // 20
	play(dealer, "TS") // 30  -> go to dealer (Seat0)
	play(pone, "TH")   // 10  (new series, pone leads)
	play(dealer, "KC") // 20  -> both out -> last card to dealer, then the show

	// Pre-show scores were small and known: pone got one go (1), dealer got one go
	// plus the last card (2). The pone had NOT won during pegging.
	dealerScoreBeforeShow := 2

	if w, ok := g.Winner(); !ok || w != pone {
		t.Fatalf("winner = (%v,%v), want (%v,true)", w, ok, pone)
	}

	// The pone's hand (three Kings = 6) pushed it from 1 to 7, past the target of 5.
	if got := g.Scores()[pone]; got != 7 {
		t.Fatalf("pone score = %d, want 7", got)
	}

	// Crucially, the dealer's hand and the crib were NOT scored after the pone won:
	// exactly one HandShown (the pone's), no CribShown, and the dealer's score is
	// unchanged from before the show.
	all := g.Events()
	var handShowns int
	var dealerWasShown bool
	for _, e := range all {
		if hs, ok := e.(HandShown); ok {
			handShowns++
			if hs.Seat == dealer {
				dealerWasShown = true
			}
		}
	}
	if handShowns != 1 {
		t.Fatalf("HandShown count = %d, want 1 (only the pone's)", handShowns)
	}
	if dealerWasShown {
		t.Error("the dealer's hand was shown; it must not be counted after the pone wins")
	}
	if hasEvent(all, func(e Event) bool { _, ok := e.(CribShown); return ok }) {
		t.Error("the crib was shown; it must not be counted after the pone wins")
	}
	if got := g.Scores()[dealer]; got != dealerScoreBeforeShow {
		t.Fatalf("dealer score = %d, want %d (unchanged by the pone's win)", got, dealerScoreBeforeShow)
	}
}
