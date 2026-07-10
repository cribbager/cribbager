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

	// The pone's hand (three Kings = 6) crosses the target of 5 from 1. The event
	// records the natural count (6), but the board score stops at the last hole:
	// the stored score is exactly the target.
	if got := g.Scores()[pone]; got != 5 {
		t.Fatalf("pone score = %d, want 5 (board stops at the target)", got)
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
			// The winning show event carries the natural count of the cards — never
			// a "points needed" remainder.
			if hs.Seat == pone && hs.Score.Total != 6 {
				t.Errorf("pone HandShown.Score.Total = %d, want 6 (the full count)", hs.Score.Total)
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

	// The dealer's hand and the crib, though never scored, ARE revealed face-up
	// (scoreless) so both players can see every hand: exactly two ShowNotCounted
	// events — the dealer's four kept cards, and the four-card crib.
	var handReveal, cribReveal *ShowNotCounted
	for _, e := range all {
		if sn, ok := e.(ShowNotCounted); ok {
			s := sn
			if s.IsCrib {
				cribReveal = &s
			} else {
				handReveal = &s
			}
		}
	}
	if handReveal == nil || cribReveal == nil {
		t.Fatalf("want a ShowNotCounted for both the dealer hand and the crib; got hand=%v crib=%v", handReveal, cribReveal)
	}
	if handReveal.Seat != dealer {
		t.Errorf("uncounted hand seat = %v, want dealer %v", handReveal.Seat, dealer)
	}
	if !cardsetEqual(handReveal.Cards, cardsFrom(t, "KC", "TC", "TD", "TS")) {
		t.Errorf("uncounted dealer hand = %v, want KC TC TD TS", handReveal.Cards)
	}
	if !cardsetEqual(cribReveal.Cards, cardsFrom(t, "2H", "2D", "2C", "2S")) {
		t.Errorf("uncounted crib = %v, want 2H 2D 2C 2S", cribReveal.Cards)
	}
}

// pegPairGame starts a preset game (default target 121) with the dealer
// (Seat0) at dealerScore, then drives the two-play sequence pone 8H, dealer 8C
// — the dealer's pair scores 2 — and returns the game and the dealer's
// CardPlayed event.
func pegPairGame(t *testing.T, dealerScore int) (*Game, CardPlayed) {
	t.Helper()
	// Deal order alternates pone (Seat1), dealer (Seat0); index 12 is the
	// starter (9S — not a Jack, so no heels).
	deal := deckWith(t,
		"8H", "8C", "2H", "3C", "4H", "5C", "9H", "TC", "QH", "QC", "KH", "KC",
		"9S")
	g := New(Options{
		Deck:  NewScriptedDeck(deal),
		Start: &Start{Scores: [2]int{dealerScore, 0}, Dealer: Seat0},
	})
	dealer, pone := Seat0, Seat1

	if _, err := g.Apply(pone, discard(t, "QH", "KH")); err != nil {
		t.Fatalf("pone discard: %v", err)
	}
	if _, err := g.Apply(dealer, discard(t, "QC", "KC")); err != nil {
		t.Fatalf("dealer discard: %v", err)
	}
	if _, err := g.Apply(pone, Play{Card: mustCard(t, "8H")}); err != nil {
		t.Fatalf("pone play: %v", err)
	}
	evs, err := g.Apply(dealer, Play{Card: mustCard(t, "8C")})
	if err != nil {
		t.Fatalf("dealer play: %v", err)
	}
	for _, e := range evs {
		if cp, ok := e.(CardPlayed); ok {
			return g, cp
		}
	}
	t.Fatal("dealer play produced no CardPlayed event")
	return nil, CardPlayed{}
}

// TestPegScoreStopsAtTarget pins the board rule for pegging: the event always
// records the natural count of the play (a pair is 2), while the SCORE — the
// board peg — stops at the target when the count crosses it. A non-crossing
// play is entirely unchanged.
func TestPegScoreStopsAtTarget(t *testing.T) {
	dealer := Seat0

	t.Run("crossing play: full event, clamped score", func(t *testing.T) {
		g, cp := pegPairGame(t, 120) // pair 2 from 120 crosses 121
		if cp.Score.Total != 2 {
			t.Errorf("CardPlayed.Score.Total = %d, want 2 (a pair is 2, always)", cp.Score.Total)
		}
		if cp.Score.Pair.Points != 2 {
			t.Errorf("CardPlayed.Score.Pair.Points = %d, want 2", cp.Score.Pair.Points)
		}
		if got := g.Scores()[dealer]; got != 121 {
			t.Errorf("dealer score = %d, want exactly 121 (board stops at the target)", got)
		}
		if w, ok := g.Winner(); !ok || w != dealer {
			t.Errorf("winner = (%v,%v), want (%v,true)", w, ok, dealer)
		}
	})

	t.Run("exact landing", func(t *testing.T) {
		g, cp := pegPairGame(t, 119) // pair 2 from 119 lands exactly on 121
		if cp.Score.Total != 2 {
			t.Errorf("CardPlayed.Score.Total = %d, want 2", cp.Score.Total)
		}
		if got := g.Scores()[dealer]; got != 121 {
			t.Errorf("dealer score = %d, want exactly 121", got)
		}
		if w, ok := g.Winner(); !ok || w != dealer {
			t.Errorf("winner = (%v,%v), want (%v,true)", w, ok, dealer)
		}
	})

	t.Run("non-crossing play is unchanged", func(t *testing.T) {
		g, cp := pegPairGame(t, 100) // pair 2 from 100: nowhere near the target
		if cp.Score.Total != 2 {
			t.Errorf("CardPlayed.Score.Total = %d, want 2", cp.Score.Total)
		}
		if got := g.Scores()[dealer]; got != 102 {
			t.Errorf("dealer score = %d, want 102", got)
		}
		if _, ok := g.Winner(); ok {
			t.Error("game over after a non-crossing play")
		}
	})
}

// TestHeelsScoreStopsAtTarget pins the board rule at the cut with a
// non-default target: his heels from target-1 still records the full 2 in the
// event, but the dealer's stored score is exactly the target. Guards against
// hardcoding 121 in the clamp.
func TestHeelsScoreStopsAtTarget(t *testing.T) {
	const target = 5
	deal := deckWith(t,
		"3H", "4C", "5H", "6C", "7H", "8C", "3D", "4D", "5D", "6D", "7D", "8D",
		"JH") // starter is a Jack -> his heels
	g := New(Options{
		Deck:        NewScriptedDeck(deal),
		TargetScore: target,
		Start:       &Start{Scores: [2]int{target - 1, 0}, Dealer: Seat0},
	})
	dealer, pone := Seat0, Seat1

	if _, err := g.Apply(pone, discard(t, "7H", "7D")); err != nil {
		t.Fatalf("pone discard: %v", err)
	}
	evs, err := g.Apply(dealer, discard(t, "8C", "8D"))
	if err != nil {
		t.Fatalf("dealer discard: %v", err)
	}

	var sawCut bool
	for _, e := range evs {
		if sc, ok := e.(StarterCut); ok {
			sawCut = true
			if sc.Heels != 2 {
				t.Errorf("StarterCut.Heels = %d, want 2 (heels are 2, always)", sc.Heels)
			}
		}
	}
	if !sawCut {
		t.Fatal("no StarterCut event")
	}
	if got := g.Scores()[dealer]; got != target {
		t.Errorf("dealer score = %d, want exactly the target %d", got, target)
	}
	if w, ok := g.Winner(); !ok || w != dealer {
		t.Errorf("winner = (%v,%v), want (%v,true)", w, ok, dealer)
	}
}

// cardsFrom builds a card slice from names.
func cardsFrom(t *testing.T, names ...string) []cribbage.Card {
	t.Helper()
	out := make([]cribbage.Card, len(names))
	for i, n := range names {
		out[i] = mustCard(t, n)
	}
	return out
}

// cardsetEqual compares two card slices as multisets (order-independent).
func cardsetEqual(a, b []cribbage.Card) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[cribbage.Card]int{}
	for _, c := range a {
		count[c]++
	}
	for _, c := range b {
		count[c]--
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return true
}
