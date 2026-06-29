package server

import (
	"math"
	"net/http"

	"github.com/cribbager/cribbager/internal/game"
)

// Player scoring statistics. Like the post-game analysis, this is computed from
// finished games' event logs (never a live session). For a player across all
// their finished games it splits the points they scored into three categories —
// pegging, hand, and crib — and reports min/max/avg/total per category.
//
// The split mirrors the engine's own scoring fold (see game.apply); every point
// the engine credits to a seat maps to exactly one category here, so for any one
// game pegging+hand+crib equals that seat's final score:
//
//   - pegging: points won during the play phase. Each CardPlayed's score and each
//     GoAwarded are credited to the seat that earned them. "His heels" (the 2 for
//     a jack starter) is also folded in here: the engine awards it to the dealer
//     at the cut, and it belongs to neither show, so it counts as a play-phase
//     point for the dealer.
//   - hand: the seat's own hand count at the show (HandShown).
//   - crib: the crib count at the show (CribShown), credited to the dealer, who
//     owns the crib.

// categoryStats summarizes a player's per-game points in one scoring category
// across their finished games. Min and Max are the smallest and largest totals
// the player scored in that category in any single game; Avg is the mean per game
// (Total / games, rounded to two decimals); Total is the sum across all games.
// With zero games every field is 0.
type categoryStats struct {
	Min   int     `json:"min"`
	Max   int     `json:"max"`
	Avg   float64 `json:"avg"`
	Total int     `json:"total"`
}

// pointStats is the per-category breakdown for one player over Games finished
// games. It is the JSON returned by the stats endpoint.
type pointStats struct {
	Games   int           `json:"games"`
	Pegging categoryStats `json:"pegging"`
	Hand    categoryStats `json:"hand"`
	Crib    categoryStats `json:"crib"`
}

// gamePoints totals the points the given seat scored in one finished game, split
// into pegging/hand/crib. It replays the scoring events exactly as the engine's
// fold does, so the three returned values sum to the seat's final game score. The
// crib and his-heels points belong to the hand's dealer, tracked from HandDealt.
func gamePoints(res Result, seat game.Seat) (pegging, hand, crib int) {
	var dealer game.Seat
	for _, ev := range res.Events {
		switch e := ev.(type) {
		case game.HandDealt:
			dealer = e.Dealer
		case game.StarterCut:
			if dealer == seat {
				pegging += e.Heels // his heels: 2 to the dealer for a jack starter
			}
		case game.CardPlayed:
			if e.Seat == seat {
				pegging += e.Score.Total
			}
		case game.GoAwarded:
			if e.Seat == seat {
				pegging += e.Points
			}
		case game.HandShown:
			if e.Seat == seat {
				hand += e.Score.Total
			}
		case game.CribShown:
			if dealer == seat {
				crib += e.Score.Total
			}
		}
	}
	return pegging, hand, crib
}

// computePointStats aggregates a player's per-category points across the given
// finished games (each must carry its Events log). Games the player isn't in are
// skipped. The zero-games case yields an all-zero pointStats (no division).
func computePointStats(results []Result, playerID string) pointStats {
	var peg, hnd, crb []int
	for _, res := range results {
		if !involves(res, playerID) {
			continue
		}
		seat := game.Seat0
		if res.PlayerIDs[1] == playerID {
			seat = game.Seat1
		}
		p, h, c := gamePoints(res, seat)
		peg = append(peg, p)
		hnd = append(hnd, h)
		crb = append(crb, c)
	}
	return pointStats{
		Games:   len(peg),
		Pegging: summarizeCategory(peg),
		Hand:    summarizeCategory(hnd),
		Crib:    summarizeCategory(crb),
	}
}

// summarizeCategory reduces a category's per-game point totals to min/max/avg/
// total. An empty input (no games) returns the zero value, avoiding a divide by
// zero on the average.
func summarizeCategory(perGame []int) categoryStats {
	if len(perGame) == 0 {
		return categoryStats{}
	}
	cs := categoryStats{Min: perGame[0], Max: perGame[0]}
	for _, v := range perGame {
		if v < cs.Min {
			cs.Min = v
		}
		if v > cs.Max {
			cs.Max = v
		}
		cs.Total += v
	}
	cs.Avg = round2(float64(cs.Total) / float64(len(perGame)))
	return cs
}

// round2 rounds an average to two decimals so the JSON is stable and readable.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// handleUserStats serves the logged-in user's lifetime scoring statistics across
// all their finished games, broken down by category (pegging/hand/crib) with
// min/max/avg/total each. Auth required; a user only ever sees their own stats.
// Path: GET /users/me/stats.
func (s *Server) handleUserStats(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	results, err := s.results.ResultsForPlayerWithEvents(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load stats")
		return
	}
	writeJSON(w, http.StatusOK, computePointStats(results, u.ID))
}
