package server

import (
	"math"
	"net/http"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Post-game analysis. This operates ONLY on a finished, stored Result (its event
// log), never on a live session — there is deliberately no in-game coaching. v1
// judges each of the player's discards against the engine's crib-aware EV optimum
// (the same evaluator the bot uses), so a finished game can be reviewed move by
// move. Pegging/play-quality analysis is a later expansion and is not done here.

// evEpsilon is the slack for calling a discard "optimal": the evaluator is
// deterministic, so a delta this small means the player's throw ties the engine's
// best (e.g. two holds with identical EV) rather than being genuinely worse.
const evEpsilon = 1e-9

// discardAnalysis is the engine's verdict on one hand's discard from the analyzed
// player's perspective. EV is exact expected points: the kept four's expected show
// value plus the signed expected crib value (+ own crib, − opponent's), which is
// exactly what the discard evaluator maximizes.
type discardAnalysis struct {
	Hand      []cribbage.Card  `json:"hand"`       // the six cards dealt to the player
	Dealer    bool             `json:"dealer"`     // true when it was the player's crib
	Throw     [2]cribbage.Card `json:"throw"`      // the two cards the player sent to the crib
	Keep      [4]cribbage.Card `json:"keep"`       // the four the player kept
	KeepEV    float64          `json:"keep_ev"`    // EV of the player's actual choice
	BestThrow [2]cribbage.Card `json:"best_throw"` // the engine's optimal discard
	BestKeep  [4]cribbage.Card `json:"best_keep"`  // the four the engine would keep
	BestEV    float64          `json:"best_ev"`    // EV of the engine's optimal discard
	DeltaEV   float64          `json:"delta_ev"`   // EV lost vs. the engine's best (≥ 0)
	Optimal   bool             `json:"optimal"`    // the player's throw matched the engine's best EV
}

// analysisSummary aggregates the per-hand verdicts.
type analysisSummary struct {
	Hands           int     `json:"hands"`            // hands the player discarded in
	OptimalDiscards int     `json:"optimal_discards"` // how many matched the engine's best EV
	TotalEVLost     float64 `json:"total_ev_lost"`    // sum of delta_ev across all hands
}

// gameAnalysisResponse is the JSON returned by the analysis endpoint.
type gameAnalysisResponse struct {
	GameID   string            `json:"game_id"`
	Seat     int               `json:"seat"` // the analyzed player's seat (0 or 1)
	Discards []discardAnalysis `json:"discards"`
	Summary  analysisSummary   `json:"summary"`
}

// analyzeDiscards reconstructs, from a finished game's event log, every hand the
// given seat discarded in and rates that discard against the engine's optimum.
//
// Reconstruction: HandDealt carries the dealer and each seat's original six cards;
// the dealer owns the crib, so the EV is computed with myCrib = (dealer == seat),
// which is what makes a "good" discard hand-specific. The seat's later Discarded
// event names the two cards it threw; the matching kept four is whatever remains.
// We match the throw (as an unordered pair) to one of the 15 ranked holds to read
// its exact EV, and compare to the top-ranked hold.
func analyzeDiscards(res Result, seat game.Seat) gameAnalysisResponse {
	out := gameAnalysisResponse{GameID: res.ID, Seat: int(seat), Discards: []discardAnalysis{}}

	var hand6 [6]cribbage.Card
	var dealer game.Seat
	haveHand := false
	for _, ev := range res.Events {
		switch e := ev.(type) {
		case game.HandDealt:
			dealer = e.Dealer
			if len(e.Hands[seat]) == 6 {
				copy(hand6[:], e.Hands[seat])
				haveHand = true
			} else {
				haveHand = false // defensive: a malformed deal is skipped, not analyzed
			}
		case game.Discarded:
			if e.Seat != seat || !haveHand {
				continue
			}
			if da, ok := rateDiscard(hand6, dealer == seat, e.Cards); ok {
				out.Discards = append(out.Discards, da)
				out.Summary.Hands++
				if da.Optimal {
					out.Summary.OptimalDiscards++
				}
				out.Summary.TotalEVLost += da.DeltaEV
			}
			haveHand = false // one discard per seat per hand
		}
	}
	out.Summary.TotalEVLost = round4(out.Summary.TotalEVLost)
	return out
}

// rateDiscard judges one discard against the engine's crib-aware EV ranking,
// reusing eval.RankDiscards (the bot's own discard logic) as the single source of
// truth — no scoring is duplicated here. ok is false only if the thrown pair isn't
// among the 15 holds of the hand, which the engine's invariants make impossible;
// it is handled defensively rather than producing a bogus verdict.
func rateDiscard(hand [6]cribbage.Card, myCrib bool, throw [2]cribbage.Card) (discardAnalysis, bool) {
	ranked := eval.RankDiscards(hand, myCrib)
	best := ranked[0]
	var chosen eval.RankedDiscard
	found := false
	for _, rd := range ranked {
		if samePair(rd.Discard, throw) {
			chosen = rd
			found = true
			break
		}
	}
	if !found {
		return discardAnalysis{}, false
	}
	delta := best.Score - chosen.Score
	if delta < 0 {
		delta = 0 // chosen is best (or a tie); never report negative EV lost
	}
	return discardAnalysis{
		Hand:      append([]cribbage.Card(nil), hand[:]...),
		Dealer:    myCrib,
		Throw:     chosen.Discard,
		Keep:      chosen.Keep,
		KeepEV:    round4(chosen.Score),
		BestThrow: best.Discard,
		BestKeep:  best.Keep,
		BestEV:    round4(best.Score),
		DeltaEV:   round4(delta),
		Optimal:   delta < evEpsilon,
	}, true
}

// samePair reports whether two discards are the same two cards regardless of order
// (the event's throw order need not match the evaluator's enumeration order).
func samePair(a, b [2]cribbage.Card) bool {
	return (a[0] == b[0] && a[1] == b[1]) || (a[0] == b[1] && a[1] == b[0])
}

// round4 rounds an EV to four decimals so the JSON is stable and free of float
// noise; the comparison that sets Optimal uses the raw delta, not this.
func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

// handleGameAnalysis serves the post-game discard analysis for one finished game,
// from the logged-in user's seat. It is strictly post-game: only games in the
// permanent result store are visible here (a live, in-progress game has no Result
// yet, so it 404s), and only a participant may read it. Path:
// GET /users/me/games/{id}/analysis.
func (s *Server) handleGameAnalysis(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	res, ok, err := s.results.ResultByID(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load game")
		return
	}
	// Unknown id, or a game in which this user holds no seat: 404 either way, so the
	// endpoint never reveals that a game it can't show exists.
	if !ok || !involves(res, u.ID) {
		writeErr(w, http.StatusNotFound, "game not found")
		return
	}
	seat := game.Seat0
	if res.PlayerIDs[1] == u.ID {
		seat = game.Seat1
	}
	writeJSON(w, http.StatusOK, analyzeDiscards(res, seat))
}
