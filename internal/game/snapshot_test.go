package game

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// TestGameStateGobRoundTrip: a GameState (whose event log is []Event, an
// interface) survives gob encode/decode intact — the serialization a persistence
// store relies on.
func TestGameStateGobRoundTrip(t *testing.T) {
	g := New(Options{Deck: NewSeededDeck(7)})
	// Play partway so the log holds a variety of event types.
	pone := other(g.dealer)
	if _, err := g.Apply(pone, Discard{Cards: [2]cribbage.Card{g.hands[pone][0], g.hands[pone][1]}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Apply(g.dealer, Discard{Cards: [2]cribbage.Card{g.hands[g.dealer][0], g.hands[g.dealer][1]}}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(g.Snapshot()); err != nil {
		t.Fatalf("gob encode: %v", err)
	}
	var got GameState
	if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("gob decode: %v", err)
	}

	orig := Restore(g.Snapshot(), NewSeededDeck(1))
	round := Restore(got, NewSeededDeck(2))
	if orig.Version() != round.Version() {
		t.Fatalf("version: %d != %d after gob round-trip", round.Version(), orig.Version())
	}
	if !reflect.DeepEqual(orig.snapshot(), round.snapshot()) {
		t.Fatal("foldable state changed across gob round-trip")
	}
	if !reflect.DeepEqual(orig.rest, round.rest) {
		t.Fatal("undealt remainder changed across gob round-trip")
	}
}

// TestSnapshotRestoreRoundTrip: a snapshot restores to identical foldable state,
// and the restored game continues the current hand deterministically — including
// the starter, which comes from the captured undealt remainder (not the log) and
// so must survive even though the restored game is given a fresh deck source.
func TestSnapshotRestoreRoundTrip(t *testing.T) {
	g := New(Options{Deck: NewSeededDeck(7)})
	snap := g.Snapshot()
	r := Restore(snap, NewSeededDeck(999)) // deliberately a DIFFERENT source

	if r.Version() != g.Version() {
		t.Fatalf("version: restored %d, want %d", r.Version(), g.Version())
	}
	if r.Scores() != g.Scores() {
		t.Fatalf("scores: restored %v, want %v", r.Scores(), g.Scores())
	}
	if !reflect.DeepEqual(r.snapshot(), g.snapshot()) {
		t.Fatal("restored foldable state != original")
	}

	// Continue the same hand on both: discard the first two cards of each hand,
	// triggering the starter cut. The starter (from the captured remainder) must
	// match despite the restored game's different deck source.
	discardBoth := func(gm *Game) {
		t.Helper()
		pone := other(gm.dealer)
		if _, err := gm.Apply(pone, Discard{Cards: [2]cribbage.Card{gm.hands[pone][0], gm.hands[pone][1]}}); err != nil {
			t.Fatalf("pone discard: %v", err)
		}
		if _, err := gm.Apply(gm.dealer, Discard{Cards: [2]cribbage.Card{gm.hands[gm.dealer][0], gm.hands[gm.dealer][1]}}); err != nil {
			t.Fatalf("dealer discard: %v", err)
		}
	}
	discardBoth(g)
	discardBoth(r)

	if !g.hasStarter || g.starter != r.starter {
		t.Fatalf("starter after continue: original %v, restored %v", g.starter, r.starter)
	}
	if !reflect.DeepEqual(g.snapshot(), r.snapshot()) {
		t.Fatal("state diverged after continuing the restored game")
	}
}
