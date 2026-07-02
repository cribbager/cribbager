package game

import (
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestHandicapStart: a preset-position game opens at the given scores with the
// given dealer, in the discard phase, with no cut for deal.
func TestHandicapStart(t *testing.T) {
	start := Start{Scores: [2]int{100, 90}, Dealer: Seat1}
	g := New(Options{Deck: NewSeededDeck(1), Start: &start})

	if g.scores != start.Scores {
		t.Fatalf("scores = %v, want %v", g.scores, start.Scores)
	}
	if g.dealer != Seat1 {
		t.Fatalf("dealer = %v, want %v", g.dealer, Seat1)
	}
	if g.phase != PhaseDiscard {
		t.Fatalf("phase = %v, want %v", g.phase, PhaseDiscard)
	}
	if _, ok := g.log[0].(Handicap); !ok {
		t.Fatalf("first event = %T, want Handicap", g.log[0])
	}
}

// TestHandicapSimulation: preset-position games run to completion under random
// legal play and uphold the engine invariants — the fold, reconciliation, and
// the play oracle — including endgame starts where the very first hand wins.
func TestHandicapSimulation(t *testing.T) {
	starts := []Start{
		{Scores: [2]int{115, 115}, Dealer: Seat0},
		{Scores: [2]int{115, 115}, Dealer: Seat1},
		{Scores: [2]int{100, 90}, Dealer: Seat0},
		{Scores: [2]int{90, 100}, Dealer: Seat1},
		{Scores: [2]int{120, 120}, Dealer: Seat0}, // first point wins
		{Scores: [2]int{60, 60}, Dealer: Seat1},
	}
	for si, start := range starts {
		for seed := int64(0); seed < 50; seed++ {
			s := start
			g := New(Options{Deck: NewSeededDeck(seed), Start: &s})
			driveRandom(t, g, seed, true)

			w, ok := g.Winner()
			if !ok {
				t.Fatalf("start %d seed %d: no winner", si, seed)
			}
			if g.scores[w] < g.target {
				t.Fatalf("start %d seed %d: winner %v has %d, below target", si, seed, w, g.scores[w])
			}
			reconcile(t, g, seed)
			foldEqual(t, g, seed)
			checkPlayOracle(t, g, seed)
		}
	}
}

// TestHandicapSnapshotRestore: a game whose log begins with Handicap survives a
// snapshot/restore round-trip, scores included.
func TestHandicapSnapshotRestore(t *testing.T) {
	start := Start{Scores: [2]int{118, 105}, Dealer: Seat0}
	g := New(Options{Deck: NewSeededDeck(3), Start: &start})
	pone := other(g.dealer)
	if _, err := g.Apply(pone, Discard{Cards: [2]cribbage.Card{g.hands[pone][0], g.hands[pone][1]}}); err != nil {
		t.Fatal(err)
	}

	r := Restore(g.Snapshot(), NewSeededDeck(999))
	if r.Scores() != g.Scores() {
		t.Fatalf("restored scores %v, want %v", r.Scores(), g.Scores())
	}
	if !reflect.DeepEqual(r.snapshot(), g.snapshot()) {
		t.Fatal("restored foldable state != original")
	}
}

// TestHandicapRejectsBadScores: a preset score at or above the target is a
// caller bug and panics.
func TestHandicapRejectsBadScores(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for Start score >= target")
		}
	}()
	New(Options{Deck: NewSeededDeck(1), Start: &Start{Scores: [2]int{121, 0}}})
}
