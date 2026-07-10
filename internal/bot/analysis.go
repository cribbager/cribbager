package bot

import (
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Post-game analysis surface for the ml bot. The production bot's networks are
// deliberately unexported (they are build artifacts, not API), but post-game
// analysis must reproduce EXACTLY the values the shipped bot maximized — so
// this file exposes those values through the same code paths the live bot
// runs (mlbot.go's discardValues/playValues), rather than letting a caller
// duplicate weights or re-derive the encoding. If the analyzer and the bot
// ever disagreed, the analysis would grade the bot's own games as mistakes.

// MLDiscardValue is one of the 15 splits of a six-card hand with the ml bot's
// value for it: the split's exact expected points (eval.RankDiscards Score:
// expected show value of the keep plus the signed expected crib value) PLUS
// the discard net's predicted pegging differential for the keep. Units are
// points per deal.
type MLDiscardValue struct {
	Discard [2]cribbage.Card
	Keep    [4]cribbage.Card
	Value   float64
}

// MLPlayValue is one legal pegging play with the ml bot's Q-value for it: the
// pegging net's predicted pegging-point differential (own points minus the
// opponent's) from playing this card's rank here to the end of the deal's
// play. Suits never score during the play, so all legal cards of a rank carry
// the same value.
type MLPlayValue struct {
	Card  cribbage.Card
	Value float64
}

// MLAnalyzer values decisions exactly like the production ml bot. It loads
// the same embedded networks the bot plays with, and its value methods are
// the bot's own internals — the bot's actual move is always the argmax of the
// returned values (first maximum in slice order, matching the bot's strict
// greater-than scan).
type MLAnalyzer struct{ b ml }

// NewMLAnalyzer builds the analyzer from the embedded production weights.
// Like seating the bot itself, it panics if the embedded weights are broken —
// that means the binary is broken.
func NewMLAnalyzer() MLAnalyzer { return MLAnalyzer{b: newML().(ml)} }

// Name is the analyzed bot's production name ("ml").
func (a MLAnalyzer) Name() string { return a.b.Name() }

// Version is the analyzed bot's algorithm version — analysis results are only
// comparable within one version, so callers should record it.
func (a MLAnalyzer) Version() string { return a.b.Version() }

// DiscardValues returns the ml bot's value for every split of a six-card
// hand, in eval.RankDiscards order (points-EV best-first — NOT necessarily
// ml-value order). ml.Discard throws the first maximum of Value in this
// order.
func (a MLAnalyzer) DiscardValues(hand [6]cribbage.Card, myCrib bool) []MLDiscardValue {
	return a.b.discardValues(hand, myCrib)
}

// PlayValues returns the ml bot's Q-value for every legal play in the view,
// in v.LegalPlays order. ml.Play plays the first maximum of Value in this
// order. The view must be a play-phase view for the seat on turn (LegalPlays
// non-empty), e.g. one reconstructed by game.ReconstructPlays.
func (a MLAnalyzer) PlayValues(v game.PlayerView) []MLPlayValue {
	return a.b.playValues(v)
}
