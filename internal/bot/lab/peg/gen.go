package peg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"

	"github.com/cribbager/cribbager/internal/bot"
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

// Generate plays full games — both seats discard like the champion and seat s
// pegs with pols[s] — and writes one Row per non-forced pegging decision, for
// both seats. Identical policies give pure self-play; a champion opponent in
// one seat gives targeted training data ("what happens against the actual
// opponent"), and the champion's own decisions are logged too — expert
// demonstrations with observed returns. Returns are computed afterwards from
// the engine's event log, the same source DealStats trusts: pegging is
// exactly the CardPlayed and GoAwarded points, so shows and heels can't leak
// into the reward.
func Generate(games int, seed int64, pols [2]Policy, w io.Writer) (Stats, error) {
	// No deferred flush: the final return flushes, and error paths abandon
	// their partial output — callers treat any error as fatal to the run.
	bw := bufio.NewWriterSize(w, 1<<20)
	enc := json.NewEncoder(bw)

	discarder := bot.Champion()
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
					if _, err := g.Apply(s, game.Discard{Cards: discarder.Discard(vs)}); err != nil {
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
		tallyStats(&st, g.Events())
		for _, d := range pending {
			evs := deals[d.deal]
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

// pegEvents extracts each deal's pegging scores, in order, from the event log.
func pegEvents(events []game.Event) [][]pegEvent {
	var deals [][]pegEvent
	for _, e := range events {
		switch e := e.(type) {
		case game.HandDealt:
			deals = append(deals, nil)
		case game.CardPlayed:
			deals[len(deals)-1] = append(deals[len(deals)-1], pegEvent{e.Seat, e.Score.Total, true})
		case game.GoAwarded:
			deals[len(deals)-1] = append(deals[len(deals)-1], pegEvent{e.Seat, e.Points, false})
		}
	}
	return deals
}

// tallyStats accumulates per-role pegging averages using bot.DealStats, the
// independently-written extractor — keeping two readers of the same event log
// honest about what "pegging points" means.
func tallyStats(st *Stats, events []game.Event) {
	for _, d := range bot.DealStats(events) {
		st.Deals++
		st.PegPerDeal[0] += float64(d.Peg[d.Dealer])
		st.PegPerDeal[1] += float64(d.Peg[1-d.Dealer])
	}
}

// Finalize converts sums to averages once generation is done.
func (st *Stats) Finalize() {
	if st.Deals > 0 {
		st.PegPerDeal[0] /= float64(st.Deals)
		st.PegPerDeal[1] /= float64(st.Deals)
	}
}
