package server

import (
	"net/http"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
)

// Stateless learning tools. Unlike a live game, these endpoints hold no state and
// touch no account — they are pure calculators over a hand the caller supplies, so
// they need no auth. They exist because analysis/teaching deliberately lives on
// dedicated learning surfaces, never inside an official game (see analysis.go).

// discardEvalRequest is the body of POST /tools/discard-eval: the six dealt cards
// and whether the crib is the caller's (dealer) or the opponent's, which flips the
// sign of the crib term and therefore the ranking.
type discardEvalRequest struct {
	Hand   []cribbage.Card `json:"hand"`
	Dealer bool            `json:"dealer"`
}

// rankedHold is one of the 15 possible holds with its expected-value breakdown,
// mirroring eval.RankedDiscard on the wire. EV is what the discard maximizes:
// the kept four's expected show value plus the signed expected crib value.
type rankedHold struct {
	Throw  [2]cribbage.Card `json:"throw"`   // the two cards sent to the crib
	Keep   [4]cribbage.Card `json:"keep"`    // the four kept
	HandEV float64          `json:"hand_ev"` // expected show value of the kept four
	CribEV float64          `json:"crib_ev"` // expected crib value, signed (+ yours, − theirs)
	EV     float64          `json:"ev"`      // hand_ev + crib_ev — the ranked score
}

// discardEvalResponse echoes the validated input and returns all 15 holds ranked
// best-first, so the client can render the ranking and locate the user's pick.
type discardEvalResponse struct {
	Hand   []cribbage.Card `json:"hand"`
	Dealer bool            `json:"dealer"`
	Holds  []rankedHold    `json:"holds"`
}

// handleDiscardEval grades a discard: it ranks all 15 holds of a supplied six-card
// hand by the same crib-aware EV the bot uses (eval.RankDiscards — the single
// source of truth, no scoring duplicated here). It is stateless and unauthenticated:
// the caller supplies the whole hand, nothing is read or written. Path:
// POST /tools/discard-eval.
func (s *Server) handleDiscardEval(w http.ResponseWriter, r *http.Request) {
	var req discardEvalRequest
	if !decodeJSON(w, r, &req) {
		return // decodeJSON already wrote a 400 (bad JSON or malformed card)
	}
	if len(req.Hand) != 6 {
		writeErr(w, http.StatusBadRequest, "hand must contain exactly 6 cards")
		return
	}
	seen := make(map[cribbage.Card]bool, 6)
	for _, c := range req.Hand {
		if seen[c] {
			writeErr(w, http.StatusBadRequest, "hand must not contain duplicate cards")
			return
		}
		seen[c] = true
	}

	var h [6]cribbage.Card
	copy(h[:], req.Hand)
	ranked := eval.RankDiscards(h, req.Dealer)

	holds := make([]rankedHold, len(ranked))
	for i, rd := range ranked {
		holds[i] = rankedHold{
			Throw:  rd.Discard,
			Keep:   rd.Keep,
			HandEV: round4(rd.EHand),
			CribEV: round4(rd.Crib),
			EV:     round4(rd.Score),
		}
	}
	writeJSON(w, http.StatusOK, discardEvalResponse{Hand: req.Hand, Dealer: req.Dealer, Holds: holds})
}
