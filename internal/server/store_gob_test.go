package server

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/game"
)

// TestRecordGobRoundTrip verifies the exact serialization PgStore relies on: a
// full Record (game state + tokens + names + bot flags + lastSeen) survives gob
// encode/decode unchanged. This runs without a database, so it guards the
// serialization even when the gated Postgres integration test is skipped.
func TestRecordGobRoundTrip(t *testing.T) {
	g := game.New(game.Options{Deck: game.NewSeededDeck(5)})
	rec := Record{
		ID:       "rec-1",
		Game:     g.Snapshot(),
		Tokens:   [2]string{"tokA", "tokB"},
		Names:    [2]string{"Alice", "Bob"},
		Bots:     [2]bool{false, true},
		LastSeen: time.Unix(1_700_000_000, 0).UTC(),
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(rec); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got Record
	if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Tokens != rec.Tokens || got.Names != rec.Names || got.Bots != rec.Bots {
		t.Fatalf("metadata changed across gob round-trip: %+v", got)
	}
	if !got.LastSeen.Equal(rec.LastSeen) {
		t.Fatalf("lastSeen changed: %v != %v", got.LastSeen, rec.LastSeen)
	}
	a := game.Restore(rec.Game, game.NewSeededDeck(1))
	b := game.Restore(got.Game, game.NewSeededDeck(2))
	if a.Version() != b.Version() || a.Scores() != b.Scores() {
		t.Fatal("restored game differs after Record gob round-trip")
	}
	if !reflect.DeepEqual(a.View(game.Seat0), b.View(game.Seat0)) {
		t.Fatal("restored view mismatch after Record gob round-trip")
	}
}
