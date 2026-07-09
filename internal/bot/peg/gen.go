package peg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Row is one training example: the encoded state, the action taken (rank
// index 0..12), and its Monte-Carlo return G — this seat's pegging points
// minus the opponent's, from this decision to the end of the deal's play.
// Game tags rows with their source game so the trainer can split
// train/validation by game (rows within a game are correlated). N is the
// number of distinct legal ranks; forced moves (N==1) are not logged — with
// no choice there is nothing to learn.
type Row struct {
	Game int       `json:"game"`
	X    []float64 `json:"x"`
	A    int       `json:"a"`
	G    float64   `json:"g"`
	N    int       `json:"n"`
}

// Discarder chooses crib throws for the generated games. It is the subset of
// bot.Bot Generate needs; this package deliberately does not import
// internal/bot (the production ML bot there imports this package).
type Discarder interface {
	Discard(v game.PlayerView) [2]cribbage.Card
}

// Stats summarizes a generation run.
type Stats struct {
	Games, Deals, Decisions int
	PegPerDeal              [2]float64 // avg pegging pts per deal, [dealer, pone]
}

// decision is a pending training row awaiting its return.
type decision struct {
	deal, play int // deal index in game, play index within deal (counts ALL plays)
	seat       game.Seat
	x          []float64
	a, n       int
}

// pegEvent is one pegging score in chronological order within a deal.
type pegEvent struct {
	seat   game.Seat
	pts    int
	isPlay bool // a CardPlayed (decisions align 1:1 with these); else GoAwarded
}

// dealPeg is one deal's pegging record: who dealt, and every pegging score in
// chronological order.
type dealPeg struct {
	dealer game.Seat
	events []pegEvent
}

// Generate plays full games — both seats discard with discard and seat s pegs
// with pols[s] — and writes one Row per non-forced pegging decision, for both
// seats. Identical policies give pure self-play; a champion opponent in one
// seat gives targeted training data ("what happens against the actual
// opponent"), and the champion's own decisions are logged too — expert
// demonstrations with observed returns. Returns are computed afterwards from
// the engine's event log: pegging is exactly the CardPlayed and GoAwarded
// points, so shows and heels can't leak into the reward.
func Generate(games int, seed int64, discard Discarder, pols [2]Policy, w io.Writer) (Stats, error) {
	// No deferred flush: the final return flushes, and error paths abandon
	// their partial output — callers treat any error as fatal to the run.
	bw := bufio.NewWriterSize(w, 1<<20)
	enc := json.NewEncoder(bw)

	var st Stats
	for gi := 0; gi < games; gi++ {
		rng := rand.New(rand.NewSource(seed ^ int64(gi)*0x9e3779b9))
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed + int64(gi))})

		var pending []decision
		deal, playIdx := -1, 0
		for {
			if _, over := g.Winner(); over {
				break
			}
			v := g.View(game.Seat0)
			switch v.Phase {
			case game.PhaseDiscard:
				deal++
				playIdx = 0
				for s := game.Seat(0); s < 2; s++ {
					vs := g.View(s)
					if len(vs.YourHand) != 6 {
						continue
					}
					if _, err := g.Apply(s, game.Discard{Cards: discard.Discard(vs)}); err != nil {
						return st, fmt.Errorf("game %d discard: %w", gi, err)
					}
				}
			case game.PhasePlay:
				seat := *v.ToPlay
				vs := g.View(seat)
				card := pols[seat].Play(vs, rng)
				if n := distinctRanks(vs); n >= 2 {
					pending = append(pending, decision{
						deal: deal, play: playIdx, seat: seat,
						x: Encode(vs), a: int(card.Rank) - 1, n: n,
					})
				}
				if _, err := g.Apply(seat, game.Play{Card: card}); err != nil {
					return st, fmt.Errorf("game %d play: %w", gi, err)
				}
				playIdx++
			default:
				return st, fmt.Errorf("game %d: unexpected phase %v", gi, v.Phase)
			}
		}

		deals := pegEvents(g.Events())
		st.Games++
		st.Deals += len(deals)
		for _, d := range deals {
			for _, e := range d.events {
				if e.seat == d.dealer {
					st.PegPerDeal[0] += float64(e.pts)
				} else {
					st.PegPerDeal[1] += float64(e.pts)
				}
			}
		}
		for _, d := range pending {
			evs := deals[d.deal].events
			ret, plays := 0.0, 0
			for _, e := range evs {
				if e.isPlay {
					plays++
				}
				if plays <= d.play { // before this decision's own play
					continue
				}
				if e.seat == d.seat {
					ret += float64(e.pts)
				} else {
					ret -= float64(e.pts)
				}
			}
			st.Decisions++
			if err := enc.Encode(Row{Game: gi, X: d.x, A: d.a, G: ret, N: d.n}); err != nil {
				return st, err
			}
		}
	}
	return st, bw.Flush()
}

// distinctRanks counts the genuinely different moves available.
func distinctRanks(v game.PlayerView) int {
	var have [Actions]bool
	n := 0
	for _, c := range v.LegalPlays {
		if !have[int(c.Rank)-1] {
			have[int(c.Rank)-1] = true
			n++
		}
	}
	return n
}

// pegEvents extracts each deal's pegging record from the event log.
func pegEvents(events []game.Event) []dealPeg {
	var deals []dealPeg
	for _, e := range events {
		switch e := e.(type) {
		case game.HandDealt:
			deals = append(deals, dealPeg{dealer: e.Dealer})
		case game.CardPlayed:
			d := &deals[len(deals)-1]
			d.events = append(d.events, pegEvent{e.Seat, e.Score.Total, true})
		case game.GoAwarded:
			d := &deals[len(deals)-1]
			d.events = append(d.events, pegEvent{e.Seat, e.Points, false})
		}
	}
	return deals
}

// PegTotals returns each deal's pegging points per seat from a game's event
// log — the same attribution Generate uses for returns, exported so tests and
// tools can hold it against independent extractors (bot.DealStats).
func PegTotals(events []game.Event) [][2]int {
	deals := pegEvents(events)
	out := make([][2]int, len(deals))
	for i, d := range deals {
		for _, e := range d.events {
			out[i][e.seat] += e.pts
		}
	}
	return out
}

// Finalize converts sums to averages once generation is done.
func (st *Stats) Finalize() {
	if st.Deals > 0 {
		st.PegPerDeal[0] /= float64(st.Deals)
		st.PegPerDeal[1] /= float64(st.Deals)
	}
}
