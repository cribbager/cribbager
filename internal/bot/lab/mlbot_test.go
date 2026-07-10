package lab

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// parityCase is one Python-generated check: a split, the indices that must be
// hot in its encoding, and the value the exported weights predict for it
// (computed by an independent numpy forward pass over the same weights file).
type parityCase struct {
	Keep    []string `json:"keep"`
	Discard []string `json:"discard"`
	Dealer  bool     `json:"dealer"`
	Ones    []int    `json:"ones"`
	Value   float64  `json:"value"`
}

// TestMLDiscardParity holds the Go encoder + inference chain equal to the
// Python side. Regenerate the fixture after retraining:
//
//	cd ml && uv run scripts/make_bot_parity.py
func TestMLDiscardParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/discard_parity.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var fx struct {
		Cases []parityCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}

	m := newMLDiscard("testdata/discard-v1.json").(*mlDiscard)
	for i, c := range fx.Cases {
		hand := parseCards(t, append(append([]string{}, c.Keep...), c.Discard...))
		x := bot.DiscardInput(hand, 4, 5, c.Dealer) // discards are the last two by construction

		for _, idx := range c.Ones {
			if x[idx] != 1 {
				t.Fatalf("case %d (%v / %v dealer=%v): index %d not hot", i, c.Keep, c.Discard, c.Dealer, idx)
			}
		}
		hot := 0
		for _, v := range x {
			if v != 0 {
				hot++
			}
		}
		if hot != len(c.Ones) {
			t.Fatalf("case %d: %d hot indices, want %d", i, hot, len(c.Ones))
		}

		if got := m.net.Forward(x)[0]; math.Abs(got-c.Value) > 1e-6 {
			t.Errorf("case %d: Go predicts %v, Python predicts %v", i, got, c.Value)
		}
	}
}

// TestMLDiscardChoosesFromHand checks the bot returns two distinct cards it
// actually holds, from both seats.
func TestMLDiscardChoosesFromHand(t *testing.T) {
	b, ok := New("ml-discard")
	if !ok {
		t.Fatal("ml-discard not registered")
	}
	hand := parseCards(t, []string{"5H", "TD", "JS", "QC", "2H", "7C"})
	for _, dealer := range []game.Seat{game.Seat0, game.Seat1} {
		v := game.PlayerView{You: game.Seat0, Dealer: dealer, YourHand: hand}
		d := b.Discard(v)
		if d[0] == d[1] {
			t.Fatalf("dealer=%v: discarded the same card twice: %v", dealer, d)
		}
		for _, c := range d {
			found := false
			for _, h := range hand {
				found = found || h == c
			}
			if !found {
				t.Fatalf("dealer=%v: discarded %v which is not in hand %v", dealer, c, hand)
			}
		}
	}
}

func parseCards(t *testing.T, texts []string) []cribbage.Card {
	t.Helper()
	out := make([]cribbage.Card, len(texts))
	for i, s := range texts {
		c, err := cribbage.ParseCard(s)
		if err != nil {
			t.Fatalf("ParseCard(%q): %v", s, err)
		}
		out[i] = c
	}
	return out
}
