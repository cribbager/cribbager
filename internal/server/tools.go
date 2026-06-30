package server

import (
	"net/http"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
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

// scoreHandRequest is the body of POST /tools/score-hand: the four cards held at
// the show plus the starter (cut) card, and whether this is a crib — which only
// changes the flush rule (a crib flushes solely when all five cards match suit).
type scoreHandRequest struct {
	Hand    []cribbage.Card `json:"hand"`    // exactly 4 cards
	Starter cribbage.Card   `json:"starter"` // the cut card
	Crib    bool            `json:"crib"`    // score as a crib (stricter flush)
}

// scoredCombo is one scoring element on the wire, mirroring hand.Combo. RunLength
// and Multiplicity are populated only for runs (a "double run" is one combo whose
// points already absorb the pair it creates), so they are omitted otherwise.
type scoredCombo struct {
	Kind         string          `json:"kind"`  // fifteen | pair | run | flush | nobs
	Cards        []cribbage.Card `json:"cards"` // the cards forming this combo
	Points       int             `json:"points"`
	RunLength    int             `json:"run_length,omitempty"`
	Multiplicity int             `json:"multiplicity,omitempty"`
}

// scoreHandResponse echoes the validated input and returns the authoritative
// total with its itemized, teachable breakdown — the engine's hand.Score, the
// single source of truth, with no scoring duplicated here.
type scoreHandResponse struct {
	Hand    []cribbage.Card `json:"hand"`
	Starter cribbage.Card   `json:"starter"`
	Crib    bool            `json:"crib"`
	Total   int             `json:"total"`
	Combos  []scoredCombo   `json:"combos"`
}

// handleScoreHand scores a cribbage show: four hand cards plus a starter, graded
// by hand.Score (the engine's own scorer — no rules duplicated here). It is
// stateless and unauthenticated: the caller supplies the whole hand, nothing is
// read or written. Path: POST /tools/score-hand.
func (s *Server) handleScoreHand(w http.ResponseWriter, r *http.Request) {
	var req scoreHandRequest
	if !decodeJSON(w, r, &req) {
		return // decodeJSON already wrote a 400 (bad JSON or malformed card)
	}
	if len(req.Hand) != 4 {
		writeErr(w, http.StatusBadRequest, "hand must contain exactly 4 cards")
		return
	}
	// A missing or out-of-range starter decodes to the zero Card (rank 0), which
	// the scorer would otherwise treat as a real card; reject it explicitly.
	if !req.Starter.Rank.Valid() || !req.Starter.Suit.Valid() {
		writeErr(w, http.StatusBadRequest, "starter must be a valid card")
		return
	}

	var hc [4]cribbage.Card
	copy(hc[:], req.Hand)
	res, err := hand.Score(hc, req.Starter, req.Crib)
	if err != nil {
		// The only error hand.Score returns is a duplicate card across the five.
		writeErr(w, http.StatusBadRequest, "hand and starter must be five distinct cards")
		return
	}

	flat := res.Combos()
	combos := make([]scoredCombo, len(flat))
	for i, c := range flat {
		combos[i] = scoredCombo{
			Kind:         c.Kind.String(),
			Cards:        c.Cards,
			Points:       c.Points,
			RunLength:    c.RunLength,
			Multiplicity: c.Multiplicity,
		}
	}
	writeJSON(w, http.StatusOK, scoreHandResponse{
		Hand:    req.Hand,
		Starter: req.Starter,
		Crib:    req.Crib,
		Total:   res.Total,
		Combos:  combos,
	})
}
