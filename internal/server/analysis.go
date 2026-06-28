package server

import (
	"net/http"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// The analysis API exposes the canonical bot's evaluator over HTTP so the web
// client's smart features — live evaluation tables (Analysis mode), move grading
// (Coach mode) — can show the same ranking the bot decides from. It is computed
// purely from the requesting seat's own View(seat): the discard ranking sees only
// your six cards and whose crib it is; the play ranking sees your hand, the public
// pile, and an opponent MODEL over the unseen cards. So it can never leak hidden
// information — it is exactly as fair as the bot, which sees the same view.

// AnalysisResponse is the ranked evaluation for whatever decision the seat faces
// now. Phase is "discard", "play", or "none" (nothing to decide at the moment).
type AnalysisResponse struct {
	Phase    string        `json:"phase"`
	Version  int           `json:"version"`
	Discards []discardRank `json:"discards,omitempty"`
	Plays    []playRank    `json:"plays,omitempty"`
}

type discardRank struct {
	Throw []cribbage.Card `json:"throw"`
	Keep  []cribbage.Card `json:"keep"`
	EHand float64         `json:"eHand"` // expected hand value of the kept four
	Crib  float64         `json:"crib"`  // expected crib value, signed
	Score float64         `json:"score"` // eHand + crib
}

type playRank struct {
	Card  cribbage.Card `json:"card"`
	Now   int           `json:"now"`   // immediate points
	Reply float64       `json:"reply"` // expected opponent reply
	Score float64       `json:"score"` // net EV
}

func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	sess, seat, ok := s.authed(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastSeen = s.now()
	writeJSON(w, http.StatusOK, analyze(sess.game.View(seat)))
}

// analyze returns the ranked evaluation for the seat's current decision.
func analyze(v game.PlayerView) AnalysisResponse {
	resp := AnalysisResponse{Phase: "none", Version: v.Version}

	switch v.Phase {
	case game.PhaseDiscard:
		// A full six-card hand means this seat has not discarded yet.
		if len(v.YourHand) == 6 {
			var h [6]cribbage.Card
			copy(h[:], v.YourHand)
			ranked := eval.RankDiscards(h, v.Dealer == v.You)
			resp.Phase = "discard"
			resp.Discards = make([]discardRank, len(ranked))
			for i, rk := range ranked {
				resp.Discards[i] = discardRank{
					Throw: []cribbage.Card{rk.Discard[0], rk.Discard[1]},
					Keep:  []cribbage.Card{rk.Keep[0], rk.Keep[1], rk.Keep[2], rk.Keep[3]},
					EHand: rk.EHand,
					Crib:  rk.Crib,
					Score: rk.Score,
				}
			}
		}
	case game.PhasePlay:
		// LegalPlays is populated by View only when it is this seat's turn.
		if len(v.LegalPlays) > 0 {
			ranked := eval.RankPlays(v)
			resp.Phase = "play"
			resp.Plays = make([]playRank, len(ranked))
			for i, rk := range ranked {
				resp.Plays[i] = playRank{Card: rk.Card, Now: rk.Mine, Reply: rk.Reply, Score: rk.Score}
			}
		}
	}
	return resp
}
