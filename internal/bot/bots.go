package bot

import (
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// --- Random: legal random moves ----------------------------------------------

// randomBot plays an arbitrary legal move. It is kept as the baseline: a floor
// for the strength ladder, and the move generator the engine's legality and
// termination tests run thousands of varied game states through (a deterministic
// bot would replay one game and cover almost nothing).
type randomBot struct{ rng *rand.Rand }

// NewRandom returns a bot that discards and plays at random.
func NewRandom(rng *rand.Rand) Bot { return &randomBot{rng: rng} }

func (b *randomBot) Name() string { return "random" }

// Version is the baseline's version. The random bot is intentionally fixed (it's
// the strength floor and the engine's move generator), so it stays at "1".
func (b *randomBot) Version() string { return "1" }

func (b *randomBot) Discard(v game.PlayerView) [2]cribbage.Card {
	h := v.YourHand
	if len(h) < 2 {
		panic("bot: Discard called with fewer than two cards in hand")
	}
	i := b.rng.Intn(len(h))
	j := b.rng.Intn(len(h) - 1)
	if j >= i {
		j++
	}
	return [2]cribbage.Card{h[i], h[j]}
}

func (b *randomBot) Play(v game.PlayerView) cribbage.Card {
	return v.LegalPlays[b.rng.Intn(len(v.LegalPlays))]
}

// --- Champion: the hand-built reference bot -----------------------------------

// championVersion is the champion's algorithm version, recorded with every
// finished game it plays. Bump it whenever the champion's logic changes.
//
// v2: calibrated pegging opponent model (discard-policy keep priors, own-throw
// exclusion, live-series go inference) — promoted at +0.66 pts/pair over v1.
// v3: win-probability objective for discard and play once either player is in
// reach of the target — promoted at +0.011 wins/pair (full game) and +0.011
// pooled over the positional fixtures over v2.
const championVersion = "3"

// champion is the hand-built reference bot: the yardstick the lab's gates
// measure challengers against, and the default opponent until ml v2 beat it
// on both promotion instruments. Both decisions maximize WIN
// PROBABILITY: away from the target that provably reduces to exact point EV
// (crib-aware discard tables, one-ply net-EV pegging against the calibrated
// opponent belief); once either player is in reach of 121, holds and plays are
// ranked by P(win) — risky when behind, safe when ahead, with no hand-written
// rules. Fully deterministic — same view in, same move out — so it takes no RNG.
//
// To improve it (or grow a new production bot): build a challenger in
// internal/bot/lab, beat a rival in a duplicate-deal comparison (bot.Compare,
// run as a go test — points margin for point-EV changes, paired win-difference
// plus the positional fixtures for score-aware ones), then promote it into this
// package. Promotion may replace the champion or register alongside it as a new
// named production bot; challengers are no longer necessarily short-lived.
type champion struct{}

func newChampion() Bot { return champion{} }

func (champion) Name() string { return ChampionName }

// Version reports the champion's algorithm version (see championVersion).
func (champion) Version() string { return championVersion }

func (champion) Discard(v game.PlayerView) [2]cribbage.Card {
	d, _ := eval.BestDiscardWin(hand6(v.YourHand), v.Dealer == v.You, v.Scores[v.You], v.Scores[1-v.You])
	return d
}

func (champion) Play(v game.PlayerView) cribbage.Card { return eval.BestPlayWin(v) }
