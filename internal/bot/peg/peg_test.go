package peg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

func card(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncode(t *testing.T) {
	starter := card(t, "9C")
	v := game.PlayerView{
		YourHand:       []cribbage.Card{card(t, "5H"), card(t, "5S"), card(t, "TD")},
		Pile:           []cribbage.Card{card(t, "4C"), card(t, "7D")}, // 7D most recent
		Count:          11,
		OpponentCards:  3,
		YourPlayed:     []cribbage.Card{card(t, "KC")},
		OpponentPlayed: []cribbage.Card{card(t, "4C"), card(t, "7D")},
		YourDiscards:   []cribbage.Card{card(t, "2H"), card(t, "3H")},
		Starter:        &starter,
	}
	x := Encode(v)
	if len(x) != Dims {
		t.Fatalf("len = %d, want %d", len(x), Dims)
	}
	for i, want := range map[int]float64{
		handOff + 4:               0.50, // two fives in hand
		handOff + 9:               0.25, // one ten
		seriesOff + 6:             1,    // slot 0 (most recent) = 7
		seriesOff + Actions + 3:   1,    // slot 1 = 4
		countOff + 11:             1,
		oppOff + 3:                1,
		seenOff + 4:               0.50, // fives: both mine
		seenOff + 3:               0.25, // the played 4
		seenOff + 8:               0.25, // starter 9
		seenOff + 12:              0.25, // my played K
		seenOff + 1:               0.25, // my discarded 2
		seriesOff + 2*Actions + 0: 0,    // series slot 2 empty
	} {
		if x[i] != want {
			t.Errorf("x[%d] = %v, want %v", i, x[i], want)
		}
	}
}

// firstTwo is the minimal legal Discarder for generator tests: throw the
// first two cards of the hand.
type firstTwo struct{}

func (firstTwo) Discard(v game.PlayerView) [2]cribbage.Card {
	return [2]cribbage.Card{v.YourHand[0], v.YourHand[1]}
}

func TestGenerate(t *testing.T) {
	var buf bytes.Buffer
	st, err := Generate(5, 3, firstTwo{}, [2]Policy{Random{}, Random{}}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if st.Games != 5 || st.Deals == 0 || st.Decisions == 0 {
		t.Fatalf("stats: %+v", st)
	}

	rows := 0
	sc := bufio.NewScanner(&buf)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r Row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("row %d: %v", rows, err)
		}
		if len(r.X) != Dims || r.A < 0 || r.A >= Actions || r.N < 2 {
			t.Fatalf("row %d malformed: len(x)=%d a=%d n=%d", rows, len(r.X), r.A, r.N)
		}
		if r.G < -60 || r.G > 60 {
			t.Fatalf("row %d: implausible return %v", rows, r.G)
		}
		// The played rank must be in hand: its hand-count feature is nonzero.
		if r.X[handOff+r.A] == 0 {
			t.Fatalf("row %d: action rank %d not in encoded hand", rows, r.A+1)
		}
		rows++
	}
	if rows != st.Decisions {
		t.Fatalf("wrote %d rows, stats say %d decisions", rows, st.Decisions)
	}
}
