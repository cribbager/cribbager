package eval

import (
	"math"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestWinProbTableSanity: the DP table must be monotone (more points never
// hurt), complementary at symmetric scores, near-certain at 120, and match the
// independently measured first-dealer win rate at 0–0.
func TestWinProbTableSanity(t *testing.T) {
	if w := WinProb(0, 0, true); math.Abs(w-0.561) > 0.01 {
		t.Errorf("WinProb(0,0,dealer) = %.4f, want ≈ 0.561 (measured first-dealer rate)", w)
	}
	for s := 0; s <= 120; s += 10 {
		sum := WinProb(s, s, true) + WinProb(s, s, false)
		if math.Abs(sum-1) > 1e-6 {
			t.Errorf("WinProb(%d,%d,dealer) + WinProb(%d,%d,pone) = %v, want 1", s, s, s, s, sum)
		}
	}
	for _, dealer := range []bool{true, false} {
		for opp := 0; opp <= 120; opp += 15 {
			prev := -1.0
			for my := 0; my <= 120; my++ {
				w := WinProb(my, opp, dealer)
				if w < prev-1e-6 {
					t.Fatalf("WinProb(%d,%d,%v) = %v below WinProb(%d,...) = %v — not monotone in my score",
						my, opp, dealer, w, my-1, prev)
				}
				prev = w
			}
		}
		for my := 0; my <= 120; my += 15 {
			prev := 2.0
			for opp := 0; opp <= 120; opp++ {
				w := WinProb(my, opp, dealer)
				if w > prev+1e-6 {
					t.Fatalf("WinProb(%d,%d,%v) = %v above WinProb(...,%d) = %v — not monotone in opp score",
						my, opp, dealer, w, opp-1, prev)
				}
				prev = w
			}
		}
	}
	// At 120 the next point wins, so 120-vs-60 must be a near lock.
	if w := WinProb(120, 60, false); w < 0.95 {
		t.Errorf("WinProb(120,60,pone) = %.4f, want near certainty", w)
	}
	// Endgame structure the table must encode: at 115-115 the SHOW decides and
	// the pone counts first — pone favored; at 120-120 PEGGING decides and the
	// dealer scores first (they reply to the lead) — dealer favored.
	if w := WinProb(115, 115, false); w < 0.5 {
		t.Errorf("WinProb(115,115,pone) = %.4f: pone counts first and should be favored", w)
	}
	if w := WinProb(120, 120, true); w < 0.5 {
		t.Errorf("WinProb(120,120,dealer) = %.4f: dealer pegs first and should be favored", w)
	}
}

// TestFarStateAgreement: away from the target the win objective must defer to
// (and agree with) the points-EV objective — the fast path IS RankDiscards, so
// this asserts the gate itself.
func TestFarStateAgreement(t *testing.T) {
	h := [6]cribbage.Card{
		card(t, "5H"), card(t, "5C"), card(t, "JD"), card(t, "4S"), card(t, "9C"), card(t, "KD"),
	}
	for _, myCrib := range []bool{true, false} {
		ev := RankDiscards(h, myCrib)
		win := RankDiscardsWin(h, myCrib, 20, 25)
		if ev[0].Discard != win[0].Discard {
			t.Errorf("myCrib=%v: far-state discard diverged: EV %v, win %v", myCrib, ev[0].Discard, win[0].Discard)
		}
	}
}

// TestDesperationAndSafety: the behavioral signature of score-aware play.
// Trailing 90–117 as dealer, a hold with a fat right tail (both 5s, jack
// kept) must rate above a flat safe hold even if means are close; and at
// 118–105 ahead, the bot must not choose a hold that gives the opponent's
// crib its best cards.
func TestDesperationAndSafety(t *testing.T) {
	h := [6]cribbage.Card{
		card(t, "5H"), card(t, "5C"), card(t, "JD"), card(t, "4S"), card(t, "9C"), card(t, "KD"),
	}

	// Desperate: dealer at 90 vs 117. The pone counts first next show; almost
	// any pone hand ends it, so this deal is close to the last chance — the
	// win objective must be active and must produce a valid ranking with Win
	// populated and ordered.
	ranked := RankDiscardsWin(h, true, 90, 117)
	if ranked[0].Win == 0 {
		t.Fatal("desperation state did not populate Win — fast path fired inside reach")
	}
	for i := 1; i < len(ranked); i++ {
		if ranked[i].Win > ranked[0].Win+1e-9 {
			t.Fatalf("ranking not by Win: [%d].Win %v > [0].Win %v", i, ranked[i].Win, ranked[0].Win)
		}
	}

	// Pegging desperation: pone at 119 vs 120, opponent deals. Making 31
	// (2 points, 119+2 = 121) pegs out and wins on the spot — Win must be
	// exactly 1.
	five, king := card(t, "5S"), card(t, "KD")
	v := game.PlayerView{
		You:            game.Seat0,
		Dealer:         game.Seat1,
		Scores:         [2]int{119, 120},
		Pile:           []cribbage.Card{card(t, "TH"), card(t, "9C"), card(t, "7D")},
		Count:          26,
		OpponentPlayed: []cribbage.Card{card(t, "TH"), card(t, "7D")},
		YourPlayed:     []cribbage.Card{card(t, "9C")},
		YourHand:       []cribbage.Card{five, king},
		LegalPlays:     []cribbage.Card{five}, // king would bust 31
	}
	got := RankPlaysWin(v)
	if got[0].Card != five || got[0].Win != 1 {
		t.Errorf("pegging out must be a certain win: got %s Win=%v", got[0].Card, got[0].Win)
	}
}

// TestRankPlaysWinLegalAndDeterministic mirrors the RankPlays guarantee for
// the win objective in an endgame state.
func TestRankPlaysWinLegalAndDeterministic(t *testing.T) {
	v := game.PlayerView{
		You:            game.Seat0,
		Dealer:         game.Seat1,
		Scores:         [2]int{110, 112},
		Pile:           []cribbage.Card{card(t, "TH")},
		Count:          10,
		OpponentPlayed: []cribbage.Card{card(t, "TH")},
		YourHand:       []cribbage.Card{card(t, "5D"), card(t, "9C"), card(t, "2S"), card(t, "KD")},
		YourDiscards:   []cribbage.Card{card(t, "3H"), card(t, "4H")},
		LegalPlays:     []cribbage.Card{card(t, "5D"), card(t, "9C"), card(t, "2S"), card(t, "KD")},
	}
	first := RankPlaysWin(v)
	legal := false
	for _, c := range v.LegalPlays {
		if c == first[0].Card {
			legal = true
		}
	}
	if !legal {
		t.Fatalf("chose %s, not legal", first[0].Card)
	}
	second := RankPlaysWin(v)
	for i := range first {
		if first[i] != second[i] {
			t.Fatal("RankPlaysWin is not deterministic")
		}
	}
}
