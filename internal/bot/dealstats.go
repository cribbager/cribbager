package bot

import (
	"github.com/cribbager/cribbager/internal/game"
)

// Increment is one scoring event within a deal, in chronological order — which
// the engine guarantees is the counting order (pegging as it happens, then pone
// hand, dealer hand, crib). The order matters for win-probability work: whoever
// crosses the target first wins, so the same point totals can decide differently
// depending on when they land.
type Increment struct {
	Seat game.Seat
	Pts  int
}

// DealStat is one deal's scoring breakdown, extracted from a game's event log.
type DealStat struct {
	Dealer     game.Seat
	Heels      int         // his-heels points (0 or 2, to the dealer)
	Peg        [2]int      // pegging points per seat, including go/last card
	Hand       [2]int      // show points per seat
	Crib       int         // crib points (to the dealer)
	Increments []Increment // every counted point, in counting order
	Complete   bool        // false when GameWon truncated the deal before the crib counted
}

// Total is the deal's combined points for one seat.
func (d DealStat) Total(s game.Seat) int {
	t := d.Peg[s] + d.Hand[s]
	if s == d.Dealer {
		t += d.Heels + d.Crib
	}
	return t
}

// DealStats splits a game's event log into per-deal scoring stats. A deal is
// Complete once its crib has counted; a deal cut short by a win (the remaining
// shows are revealed but never scored) stays incomplete, so per-deal averages
// (Colvert par, outcome distributions) can be taken over complete deals only.
func DealStats(events []game.Event) []DealStat {
	var deals []DealStat
	cur := -1
	add := func(seat game.Seat, pts int) {
		if pts == 0 {
			return
		}
		d := &deals[cur]
		d.Increments = append(d.Increments, Increment{Seat: seat, Pts: pts})
	}

	for _, e := range events {
		switch e := e.(type) {
		case game.HandDealt:
			deals = append(deals, DealStat{Dealer: e.Dealer})
			cur = len(deals) - 1
		case game.StarterCut:
			if cur < 0 {
				continue
			}
			deals[cur].Heels = e.Heels
			add(deals[cur].Dealer, e.Heels)
		case game.CardPlayed:
			deals[cur].Peg[e.Seat] += e.Score.Total
			add(e.Seat, e.Score.Total)
		case game.GoAwarded:
			deals[cur].Peg[e.Seat] += e.Points
			add(e.Seat, e.Points)
		case game.HandShown:
			deals[cur].Hand[e.Seat] += e.Score.Total
			add(e.Seat, e.Score.Total)
		case game.CribShown:
			deals[cur].Crib += e.Score.Total
			add(deals[cur].Dealer, e.Score.Total)
			deals[cur].Complete = true
		}
	}
	return deals
}
