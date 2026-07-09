package peg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
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

// TestPegEventsMatchDealStats holds this package's event walker equal to
// bot.DealStats, the independently written extractor: per deal and seat, the
// summed pegging points must agree exactly.
func TestPegEventsMatchDealStats(t *testing.T) {
	g := game.New(game.Options{Deck: game.NewSeededDeck(7)})
	rng := rand.New(rand.NewSource(7))
	discarder := bot.Champion()
	for {
		if _, over := g.Winner(); over {
			break
		}
		v := g.View(game.Seat0)
		switch v.Phase {
		case game.PhaseDiscard:
			for s := game.Seat(0); s < 2; s++ {
				if vs := g.View(s); len(vs.YourHand) == 6 {
					if _, err := g.Apply(s, game.Discard{Cards: discarder.Discard(vs)}); err != nil {
						t.Fatal(err)
					}
				}
			}
		case game.PhasePlay:
			seat := *v.ToPlay
			if _, err := g.Apply(seat, game.Play{Card: Random{}.Play(g.View(seat), rng)}); err != nil {
				t.Fatal(err)
			}
		}
	}

	mine := pegEvents(g.Events())
	ref := bot.DealStats(g.Events())
	if len(mine) != len(ref) {
		t.Fatalf("deal count: walker %d, DealStats %d", len(mine), len(ref))
	}
	for d := range mine {
		var got [2]int
		for _, e := range mine[d] {
			got[e.seat] += e.pts
		}
		if got != ref[d].Peg {
			t.Errorf("deal %d: walker peg %v, DealStats %v", d, got, ref[d].Peg)
		}
	}
}

func TestGenerate(t *testing.T) {
	var buf bytes.Buffer
	st, err := Generate(5, 3, Random{}, &buf)
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
