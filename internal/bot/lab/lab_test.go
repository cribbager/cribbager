package lab

import (
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/game"
)

// TestCandidatePlaysTheChampion exercises the harness end to end: the template
// challenger is registered, builds, and plays a full legal game against the
// shipped champion that terminates with a winner at or past the target.
func TestCandidatePlaysTheChampion(t *testing.T) {
	cand, ok := New("candidate")
	if !ok {
		t.Fatal("candidate not registered in the lab")
	}
	champ := bot.Champion()
	for seed := int64(0); seed < 20; seed++ {
		res, err := bot.PlayGame(cand, champ, game.NewSeededDeck(seed))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if res.Scores[res.Winner] < 121 {
			t.Fatalf("seed %d: winner has %d", seed, res.Scores[res.Winner])
		}
	}
}

// TestUnchangedCandidateMirrorsChampion documents the template invariant: an
// un-edited candidate embeds the champion, so it must choose the same moves —
// a self-play game and a candidate-vs-champion game from the same deal are
// identical. (Once you override Discard/Play, this no longer holds — that is the
// point.)
func TestUnchangedCandidateMirrorsChampion(t *testing.T) {
	cand, _ := New("candidate")
	champ := bot.Champion()
	for seed := int64(0); seed < 20; seed++ {
		ref, err := bot.PlayGame(bot.Champion(), champ, game.NewSeededDeck(seed))
		if err != nil {
			t.Fatal(err)
		}
		got, err := bot.PlayGame(cand, champ, game.NewSeededDeck(seed))
		if err != nil {
			t.Fatal(err)
		}
		if got != ref {
			t.Fatalf("seed %d: unchanged candidate diverged from champion (%+v vs %+v)", seed, got, ref)
		}
	}
}
