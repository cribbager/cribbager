package game

import (
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// DeckSource produces a freshly shuffled 52-card deck on demand. The engine
// pulls one for the cut-for-deal and one for each deal, so all randomness enters
// here. Tests inject a seeded or scripted source; the server injects a
// CSPRNG-backed one.
type DeckSource interface {
	Shuffle() []cribbage.Card
}

// SeededDeck is a reproducible DeckSource for tests and simulations.
type SeededDeck struct{ rng *rand.Rand }

// NewSeededDeck returns a deterministic source seeded with seed.
func NewSeededDeck(seed int64) *SeededDeck { return &SeededDeck{rng: rand.New(rand.NewSource(seed))} }

func (s *SeededDeck) Shuffle() []cribbage.Card {
	d := cribbage.Deck()
	s.rng.Shuffle(len(d), func(i, j int) { d[i], d[j] = d[j], d[i] })
	return d
}

// ScriptedDeck returns predetermined decks in order — used to set up exact
// scenarios in tests. It panics if asked for more decks than were scripted.
type ScriptedDeck struct {
	decks [][]cribbage.Card
	i     int
}

// NewScriptedDeck builds a source that hands out the given decks in order.
func NewScriptedDeck(decks ...[]cribbage.Card) *ScriptedDeck { return &ScriptedDeck{decks: decks} }

func (s *ScriptedDeck) Shuffle() []cribbage.Card {
	if s.i >= len(s.decks) {
		panic("game: scripted deck source exhausted")
	}
	d := s.decks[s.i]
	s.i++
	out := make([]cribbage.Card, len(d))
	copy(out, d)
	return out
}
