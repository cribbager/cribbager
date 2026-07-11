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
// sign of the crib term and therefore the ranking. The scores are optional
// (pointers so 0 is distinguishable from absent) and must come as a pair: with
// them the response adds each hold's win probability and the game-situation
// block, ranked by eval.RankDiscardsWin instead of eval.RankDiscards.
type discardEvalRequest struct {
	Hand     []cribbage.Card `json:"hand"`
	Dealer   bool            `json:"dealer"`
	MyScore  *int            `json:"my_score"`
	OppScore *int            `json:"opp_score"`
}

// rankedHold is one of the 15 possible holds with its expected-value breakdown,
// mirroring eval.RankedDiscard on the wire. EV is what the discard maximizes:
// the kept four's expected show value plus the signed expected crib value. Win is
// this hold's win probability — meaningful only when the request carried scores
// AND the situation is in reach of 121 (situation.endgame true); otherwise it is
// a true 0, kept on the wire (never omitempty) so clients needn't special-case a
// missing field (see TestDeltaNumericFieldsAlwaysPresent for the convention).
//
// The distribution block (HandDist + the three derived upside summaries) exposes
// the "upside" dimension EV alone hides: two holds with the same mean can have very
// different right tails, and a fat tail is what you want late in a game when only a
// big hand counts you out. It describes the HAND score only (the kept four over
// every starter); the crib is a separate signed term with its own distribution,
// deliberately NOT convolved in here — hand+crib convolution is a documented future
// extension. All four fields are always present (never omitempty), same convention
// as Win.
type rankedHold struct {
	Throw  [2]cribbage.Card `json:"throw"`   // the two cards sent to the crib
	Keep   [4]cribbage.Card `json:"keep"`    // the four kept
	HandEV float64          `json:"hand_ev"` // expected show value of the kept four
	CribEV float64          `json:"crib_ev"` // expected crib value, signed (+ yours, − theirs)
	EV     float64          `json:"ev"`      // hand_ev + crib_ev — the ranked score
	Win    float64          `json:"win"`     // P(win) with this hold (0 unless endgame)

	// HandDist is the exact probability of each hand show score 0..29 for the kept
	// four, swept over every possible starter — the same sweep as HandEV, so its
	// probability-weighted mean equals HandEV. A fixed-length array, always present,
	// with entries summing to ~1 (rounded per-entry for the wire).
	HandDist [eval.HandScoreDistSize]float64 `json:"hand_dist"`
	// HandPGE12 is P(hand score ≥ 12): the chance of a "big" hand once the starter is
	// cut. 12 is roughly the top decile of hand-only outcomes (a random keep averages
	// ~4–5), so it is the threshold at which a hand is large enough to matter when you
	// are chasing a count-out. This is the headline right-tail / upside metric.
	HandPGE12 float64 `json:"hand_p_ge_12"`
	// HandP90 is the 90th-percentile hand score: the smallest score s with P(hand ≤ s)
	// ≥ 0.90. A robust "realistic ceiling" — the good-cut outcome you can actually
	// expect, less noisy than the absolute maximum.
	HandP90 int `json:"hand_p90"`
	// HandCeiling is the maximum hand score attainable over any starter (the highest
	// score with nonzero probability) — the best case if the cut cooperates.
	HandCeiling int `json:"hand_ceiling"`
}

// bigHandThreshold is the score at or above which a hand counts as "big" for the
// upside metric HandPGE12 — see that field's doc for the rationale.
const bigHandThreshold = 12

// handUpside derives the three upside summaries from a hand-score distribution
// (probabilities over scores 0..29). It returns the per-entry-rounded histogram for
// the wire alongside P(score ≥ bigHandThreshold), the 90th-percentile score, and the
// ceiling (highest score with any probability). Deriving these server-side keeps the
// definitions in one place so no two clients disagree on what "upside" means.
func handUpside(dist [eval.HandScoreDistSize]float64) (wire [eval.HandScoreDistSize]float64, pGE12 float64, p90, ceiling int) {
	cum := 0.0
	p90 = -1
	for s, p := range dist {
		wire[s] = round6(p)
		if s >= bigHandThreshold {
			pGE12 += p
		}
		cum += p
		if p90 < 0 && cum >= 0.90 {
			p90 = s
		}
		if p > 0 {
			ceiling = s
		}
	}
	if p90 < 0 { // guard: an empty (all-zero) distribution has no percentile
		p90 = 0
	}
	return wire, round6(pGE12), p90, ceiling
}

// discardSituation describes the game position the discard is evaluated in, for
// requests that carry scores. Endgame reports whether the win-probability
// objective is active (someone is in reach of 121); when false every hold's win
// is 0 by construction and the ranking is the points-EV order — the client must
// present that state, not a column of zeros.
type discardSituation struct {
	MyScore  int     `json:"my_score"`
	OppScore int     `json:"opp_score"`
	MyNeed   int     `json:"my_need"`  // points to 121 for the caller
	OppNeed  int     `json:"opp_need"` // points to 121 for the opponent
	WinProb  float64 `json:"win_prob"` // P(caller wins) at these scores, before the deal
	Endgame  bool    `json:"endgame"`  // win objective active (someone in reach of 121)
}

// discardEvalResponse echoes the validated input and returns all 15 holds ranked
// best-first, so the client can render the ranking and locate the user's pick.
// The ranking objective follows the request: points EV without scores, win
// probability (falling back to points EV far from the end) with them. Situation
// is present exactly when the request carried scores.
type discardEvalResponse struct {
	Hand      []cribbage.Card   `json:"hand"`
	Dealer    bool              `json:"dealer"`
	Holds     []rankedHold      `json:"holds"`
	Situation *discardSituation `json:"situation,omitempty"`
}

// handleDiscardEval grades a discard: it ranks all 15 holds of a supplied six-card
// hand by the same crib-aware evaluation the bot uses (eval.RankDiscards, or
// eval.RankDiscardsWin when the request carries scores — the single source of
// truth either way, no scoring duplicated here). It is stateless and
// unauthenticated: the caller supplies the whole hand, nothing is read or
// written. Path: POST /tools/discard-eval.
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
	if (req.MyScore == nil) != (req.OppScore == nil) {
		writeErr(w, http.StatusBadRequest, "my_score and opp_score must be given together")
		return
	}
	if req.MyScore != nil {
		for _, v := range []int{*req.MyScore, *req.OppScore} {
			if v < 0 || v > 120 {
				writeErr(w, http.StatusBadRequest, "scores must be between 0 and 120")
				return
			}
		}
	}

	var h [6]cribbage.Card
	copy(h[:], req.Hand)
	var ranked []eval.RankedDiscard
	var situation *discardSituation
	if req.MyScore != nil {
		my, opp := *req.MyScore, *req.OppScore
		ranked = eval.RankDiscardsWin(h, req.Dealer, my, opp)
		situation = &discardSituation{
			MyScore:  my,
			OppScore: opp,
			MyNeed:   121 - my,
			OppNeed:  121 - opp,
			WinProb:  round4(eval.WinProb(my, opp, req.Dealer)),
			Endgame:  eval.InReach(my, opp, req.Dealer),
		}
	} else {
		ranked = eval.RankDiscards(h, req.Dealer)
	}

	holds := make([]rankedHold, len(ranked))
	for i, rd := range ranked {
		// The distribution sweeps the same starters as EHand (seen = the six dealt
		// cards), so its mean equals HandEV — the histogram is the same computation
		// kept un-collapsed. Derive the upside summaries from it here.
		dist, pGE12, p90, ceiling := handUpside(eval.HandValueDist(rd.Keep, h[:]))
		holds[i] = rankedHold{
			Throw:       rd.Discard,
			Keep:        rd.Keep,
			HandEV:      round4(rd.EHand),
			CribEV:      round4(rd.Crib),
			EV:          round4(rd.Score),
			Win:         round6(rd.Win),
			HandDist:    dist,
			HandPGE12:   pGE12,
			HandP90:     p90,
			HandCeiling: ceiling,
		}
	}
	writeJSON(w, http.StatusOK, discardEvalResponse{Hand: req.Hand, Dealer: req.Dealer, Holds: holds, Situation: situation})
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
