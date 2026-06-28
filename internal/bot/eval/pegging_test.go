package eval

import (
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

func card(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPlayValueMatchesScorer(t *testing.T) {
	pile := []cribbage.Card{card(t, "4H"), card(t, "5C")}
	for _, name := range []string{"6D", "KH", "5S", "AC"} {
		c := card(t, name)
		want, _ := pegging.Score(pile, c)
		if got := PlayValue(pile, c); got != want.Total {
			t.Errorf("PlayValue(%s) = %d, want %d", name, got, want.Total)
		}
	}
}

func TestBestPlayNetEVReturnsLegalCard(t *testing.T) {
	v := game.PlayerView{
		Pile:       []cribbage.Card{card(t, "TH")},
		Count:      10,
		LegalPlays: []cribbage.Card{card(t, "5D"), card(t, "9C"), card(t, "2S")},
	}
	got := BestPlayNetEV(v)
	legal := false
	for _, c := range v.LegalPlays {
		if c == got {
			legal = true
		}
	}
	if !legal {
		t.Errorf("BestPlayNetEV chose %s, not in legal plays", got)
	}
}
