// Package bot provides cribbage bots of increasing strength and a bot-vs-bot
// match runner. A bot decides only from its seat's view, so it is fair and
// testable; the engine still validates every move.
package bot

import (
	"fmt"
	"math/rand"
	"sort"

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

// ChampionName names the champion — the hand-built reference bot the
// improvement program measures against (internal/bot/lab gates compare
// challengers to it by default).
const ChampionName = "champion"

// DefaultName is the name of the default opponent — the bot seated when a
// caller doesn't pick one. It became the ML bot when ml v2 beat the champion
// on both promotion instruments (+0.75 pts/pair and +0.011 wins/pair over
// 10,000 duplicate deal-pairs; docs/research/ml-bot chapters 5–7). The
// champion remains available by name and stays the lab's reference.
const DefaultName = "ml"

// registry is the table of PRODUCTION bots: the bots the server may seat and the
// CLI may pick, each built by name with an RNG (random uses it; deterministic
// bots ignore it). A new bot ships by adding a line here — that no longer means
// replacing the champion; production bots coexist, and DefaultName is merely the
// one seated by default. Bots under development live in internal/bot/lab and are
// absent here, so a challenger can never reach the server until it is promoted.
var registry = map[string]func(rng *rand.Rand) Bot{
	"random":     func(rng *rand.Rand) Bot { return NewRandom(rng) },
	ChampionName: func(*rand.Rand) Bot { return newChampion() },
	DefaultName:  func(*rand.Rand) Bot { return newML() },
}

// Names lists the production bots, sorted, so the set is stable and printable
// (e.g. in an unknown-bot error or a GET /bots listing).
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Valid reports whether name is a production bot (one the server may seat).
func Valid(name string) bool { _, ok := registry[name]; return ok }

// New builds a production bot by name with the given RNG (used by random for its
// choices; the champion is deterministic and ignores it). An unknown name is an
// error — callers validating external input should surface it as a 4xx.
// Challengers under development are built via internal/bot/lab, not here.
func New(name string, rng *rand.Rand) (Bot, error) {
	make, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("bot: unknown bot %q (have %v)", name, Names())
	}
	return make(rng), nil
}

// Champion returns the champion — the hand-built reference bot. The lab's
// gates and the training-data generators want this specific bot regardless
// of what the server seats by default; callers wanting "the default
// opponent" should build DefaultName via New instead.
func Champion() Bot { return newChampion() }

// hand6 converts a six-card view hand into a fixed array for the evaluators.
func hand6(cards []cribbage.Card) [6]cribbage.Card {
	var h [6]cribbage.Card
	copy(h[:], cards)
	return h
}
