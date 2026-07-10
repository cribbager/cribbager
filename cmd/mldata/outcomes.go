package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/eval"
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

// The win mode (docs/research/ml-bot chapter 8). Labels each chosen split
// with the deal's WIN-PROBABILITY DELTA: WinProb at the end of the deal
// (exact 1 or 0 when the deal ends the game — so unlike the points target,
// nothing is censored) minus WinProb at its start, from the decider's seat.
// Bootstrapping through the WinProb table instead of waiting for the real
// game outcome is the variance move that makes a win target trainable at
// all; the price is inheriting the table's own (champion self-play) biases.
// Inputs are emitted pre-encoded (bot.DiscardInputWin) — the chapter 4
// pattern: the encoder lives only where inference lives.
//
// Behavior: the production ml bot's own discards and pegging, with ε random
// splits for exploration — on-policy for the bot this data will improve.
type winRow struct {
	Game int       `json:"game"`
	X    []float64 `json:"x"`
	WP   float64   `json:"wp"`
}

// pendingWin is one logged discard decision awaiting its win delta.
type pendingWin struct {
	deal int
	seat game.Seat
	x    []float64
}

func generateWin(w io.Writer, games int, seed int64, eps float64) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	enc := json.NewEncoder(bw)

	mlBot, err := bot.New("ml", rand.New(rand.NewSource(seed)))
	if err != nil {
		return err
	}

	rows := 0
	for gi := 0; gi < games; gi++ {
		rng := rand.New(rand.NewSource(seed ^ int64(gi)*0x9e3779b9))
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed + int64(gi))})

		var pending []pendingWin
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
					d := mlBot.Discard(vs)
					if rng.Float64() < eps {
						d = randomSplit(vs.YourHand, rng)
					}
					pending = append(pending, pendingWin{
						deal: deal, seat: s, x: encodeWinDecision(vs, d),
					})
					if _, err := g.Apply(s, game.Discard{Cards: d}); err != nil {
						return fmt.Errorf("game %d discard: %w", gi, err)
					}
				}
			case game.PhasePlay:
				seat := *v.ToPlay
				if _, err := g.Apply(seat, game.Play{Card: mlBot.Play(g.View(seat))}); err != nil {
					return fmt.Errorf("game %d play: %w", gi, err)
				}
			default:
				return fmt.Errorf("game %d: unexpected phase %v", gi, v.Phase)
			}
		}

		// Walk the deals with running scores to compute each decision's
		// win-probability delta from its decider's seat.
		stats := bot.DealStats(g.Events())
		type span struct{ start, end [2]int }
		spans := make([]span, len(stats))
		var running [2]int
		for di, d := range stats {
			spans[di].start = running
			for _, inc := range d.Increments {
				running[inc.Seat] += inc.Pts
			}
			spans[di].end = running
		}
		for _, p := range pending {
			d, sp := stats[p.deal], spans[p.deal]
			me, opp := p.seat, 1-p.seat
			wpStart := eval.WinProb(sp.start[me], sp.start[opp], p.seat == d.Dealer)
			wpEnd := eval.WinProb(sp.end[me], sp.end[opp], p.seat != d.Dealer) // deal rotates
			rows++
			if err := enc.Encode(winRow{Game: gi, X: p.x, WP: wpEnd - wpStart}); err != nil {
				return err
			}
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d games, %d rows (win-delta target, nothing censored)\n", games, rows)
	return nil
}

// encodeWinDecision builds the win-net input for throwing d from the hand in
// vs, looking up the split's exact expected points.
func encodeWinDecision(vs game.PlayerView, d [2]cribbage.Card) []float64 {
	h := vs.YourHand
	dealer := vs.Dealer == vs.You
	var hand [6]cribbage.Card
	copy(hand[:], h)
	score := 0.0
	for _, rd := range eval.RankDiscards(hand, dealer) {
		if rd.Discard == d || (rd.Discard[0] == d[1] && rd.Discard[1] == d[0]) {
			score = rd.Score
			break
		}
	}
	// The encoder wants discard positions: rebuild the hand with the thrown
	// two last.
	ordered := make([]cribbage.Card, 0, 6)
	for _, c := range h {
		if c != d[0] && c != d[1] {
			ordered = append(ordered, c)
		}
	}
	ordered = append(ordered, d[0], d[1])
	return bot.DiscardInputWin(ordered, 4, 5, dealer, vs.Scores[vs.You], vs.Scores[1-vs.You], score)
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
