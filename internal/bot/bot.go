// Package bot provides cribbage bots of increasing strength and a bot-vs-bot
// match runner. A bot decides only from its seat's view, so it is fair and
// testable; the engine still validates every move.
package bot

import (
	"fmt"
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Bot chooses moves for one seat from that seat's view.
type Bot interface {
	Name() string
	// Version is the bot's algorithm version, recorded with each finished game
	// so a later replay/analysis knows which bot played it. Different bots report
	// different versions; a bot bumps its own when its logic changes.
	Version() string
	Discard(v game.PlayerView) [2]cribbage.Card
	Play(v game.PlayerView) cribbage.Card
}

// DefaultName is the name of the shipped bot — the single opponent the product
// plays against and the one we keep improving. It is a role name ("the
// champion"), deliberately not tied to the current algorithm, so the
// implementation can change without the name rippling through callers.
const DefaultName = "champion"

// Version is the shipped (champion) bot's algorithm version. Bump it whenever the
// champion's logic changes, so finished games record which bot version played them.
//
// v2: calibrated pegging opponent model (discard-policy keep priors, own-throw
// exclusion, live-series go inference) — promoted at +0.66 pts/pair over v1.
// v3: win-probability objective for discard and play once either player is in
// reach of the target — promoted at +0.011 wins/pair (full game) and +0.011
// pooled over the positional fixtures over v2.
const Version = "3"

// Names lists the production bots: the champion (the default opponent) and the
// legal-random baseline, kept as the evaluator's noise floor and the engine's
// legality/termination move generator. Bots under development live in
// internal/bot/lab, not here, so they can never reach the server.
func Names() []string { return []string{"random", DefaultName} }

// New builds a production bot by name with the given RNG (used by random for its
// choices; the champion is deterministic and ignores it). Challengers under
// development are built via internal/bot/lab, not here.
func New(name string, rng *rand.Rand) (Bot, error) {
	switch name {
	case "random":
		return NewRandom(rng), nil
	case DefaultName:
		return newChampion(), nil
	default:
		return nil, fmt.Errorf("bot: unknown bot %q (have %v)", name, Names())
	}
}

// Champion returns the shipped bot. Callers that just want "the opponent" should
// use this instead of hardcoding a name.
func Champion() Bot { return newChampion() }

// hand6 converts a six-card view hand into a fixed array for the evaluators.
func hand6(cards []cribbage.Card) [6]cribbage.Card {
	var h [6]cribbage.Card
	copy(h[:], cards)
	return h
}
