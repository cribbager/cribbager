package bot

import (
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestMLBot covers the production ML bot's contract: it builds from the
// embedded weights, discards exactly like the champion (same evaluator), and
// pegs deterministically with a legal card.
func TestMLBot(t *testing.T) {
	b, err := New("ml", rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatal(err)
	}
	if b.Name() != "ml" || b.Version() == "" {
		t.Fatalf("identity: %q v%q", b.Name(), b.Version())
	}

	cards := func(ss ...string) []cribbage.Card {
		out := make([]cribbage.Card, len(ss))
		for i, s := range ss {
			c, err := cribbage.ParseCard(s)
			if err != nil {
				t.Fatal(err)
			}
			out[i] = c
		}
		return out
	}

	dv := game.PlayerView{
		You: game.Seat0, Dealer: game.Seat0,
		YourHand: cards("5H", "TD", "JS", "QC", "2H", "7C"),
	}
	if got, want := b.Discard(dv), Champion().Discard(dv); got != want {
		t.Errorf("Discard = %v, champion discards %v — must be identical", got, want)
	}

	pv := game.PlayerView{
		You: game.Seat0, Dealer: game.Seat1,
		YourHand:   cards("5H", "TD", "QC", "7C"),
		Pile:       cards("8D"),
		Count:      8,
		LegalPlays: cards("5H", "TD", "QC", "7C"),
	}
	first := b.Play(pv)
	legal := false
	for _, c := range pv.LegalPlays {
		legal = legal || c == first
	}
	if !legal {
		t.Fatalf("Play returned %v, not in legal plays %v", first, pv.LegalPlays)
	}
	if again := b.Play(pv); again != first {
		t.Errorf("Play not deterministic: %v then %v", first, again)
	}
}
