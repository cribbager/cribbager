package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// The outcomes mode (docs/research/ml-bot chapter 7). Where the exact mode
// labels every split with the evaluator's expectation — exact but blind to
// pegging by construction — this mode labels the ONE split actually chosen
// with what the whole deal then delivered: own deal points minus the
// opponent's, pegging included, under the production ML bot's pegging.
// Complete-but-noisy versus exact-but-partial; volume pays for the noise.
//
// Behavior policy: champion discards with an ε of uniformly random splits —
// the exploration that lets the trainer see what bad and weird keeps lead
// to, exactly as ε-greedy did for pegging. Both seats log a row per deal.
// Deals truncated by a game ending are censored (their outcome was never
// fully realized); the resulting slight tilt away from endgame deals is
// acceptable for this deliberately score-blind objective.
type outcomeRow struct {
	Game    int      `json:"game"`
	Keep    []string `json:"keep"`
	Discard []string `json:"discard"`
	Dealer  bool     `json:"dealer"`
	G       float64  `json:"g"`
	// PegDiff is the deal's realized pegging differential alone (own pegging
	// minus opponent's) — the residual target: shows and crib have exact
	// expectations the trainer shouldn't relearn through noise, pegging is
	// the one component the exact evaluator cannot rank per hold.
	PegDiff float64 `json:"peg_diff"`
}

// pendingDiscard is one logged discard decision awaiting its deal outcome.
type pendingDiscard struct {
	deal   int
	seat   game.Seat
	keep   []string
	disc   []string
	dealer bool
}

func generateOutcomes(w io.Writer, games int, seed int64, eps float64) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	enc := json.NewEncoder(bw)

	champ := bot.Champion()
	pegger, err := bot.New("ml", rand.New(rand.NewSource(seed)))
	if err != nil {
		return err
	}

	rows, censored := 0, 0
	for gi := 0; gi < games; gi++ {
		rng := rand.New(rand.NewSource(seed ^ int64(gi)*0x9e3779b9))
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed + int64(gi))})

		var pending []pendingDiscard
		deal := -1
		for {
			if _, over := g.Winner(); over {
				break
			}
			v := g.View(game.Seat0)
			switch v.Phase {
			case game.PhaseDiscard:
				deal++
				for s := game.Seat(0); s < 2; s++ {
					vs := g.View(s)
					if len(vs.YourHand) != 6 {
						continue
					}
					d := champ.Discard(vs)
					if rng.Float64() < eps {
						d = randomSplit(vs.YourHand, rng)
					}
					pending = append(pending, pendingDiscard{
						deal: deal, seat: s,
						keep: keepStrings(vs.YourHand, d), disc: []string{d[0].String(), d[1].String()},
						dealer: vs.Dealer == s,
					})
					if _, err := g.Apply(s, game.Discard{Cards: d}); err != nil {
						return fmt.Errorf("game %d discard: %w", gi, err)
					}
				}
			case game.PhasePlay:
				seat := *v.ToPlay
				if _, err := g.Apply(seat, game.Play{Card: pegger.Play(g.View(seat))}); err != nil {
					return fmt.Errorf("game %d play: %w", gi, err)
				}
			default:
				return fmt.Errorf("game %d: unexpected phase %v", gi, v.Phase)
			}
		}

		stats := bot.DealStats(g.Events())
		for _, p := range pending {
			d := stats[p.deal]
			if !d.Complete {
				censored++
				continue
			}
			rows++
			if err := enc.Encode(outcomeRow{
				Game: gi, Keep: p.keep, Discard: p.disc, Dealer: p.dealer,
				G:       float64(d.Total(p.seat) - d.Total(1-p.seat)),
				PegDiff: float64(d.Peg[p.seat] - d.Peg[1-p.seat]),
			}); err != nil {
				return err
			}
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d games, %d rows (%d censored by game end)\n", games, rows, censored)
	return nil
}

// randomSplit throws two distinct uniformly random cards.
func randomSplit(h []cribbage.Card, rng *rand.Rand) [2]cribbage.Card {
	i := rng.Intn(len(h))
	j := rng.Intn(len(h) - 1)
	if j >= i {
		j++
	}
	return [2]cribbage.Card{h[i], h[j]}
}

// keepStrings renders the four cards of h not thrown in d.
func keepStrings(h []cribbage.Card, d [2]cribbage.Card) []string {
	out := make([]string, 0, 4)
	for _, c := range h {
		if c != d[0] && c != d[1] {
			out = append(out, c.String())
		}
	}
	return out
}
