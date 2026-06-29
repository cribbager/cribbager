package server

import (
	"net/http"
)

// Post-game replay. Like the analysis endpoint, this operates ONLY on a finished,
// stored Result (its full event log), never on a live session. Replay reveals
// hidden information — BOTH seats' dealt hands and BOTH players' discards — but
// strictly for a participant viewing their OWN finished game. It uses the
// full-visibility projection (projectReplayEvents); the live per-seat stream and
// its redacting projection (projectEvent) are deliberately left untouched.

// replaySeat is one seat's roster entry in the replay response.
type replaySeat struct {
	Name string `json:"name"`
	Bot  bool   `json:"bot"` // true when this seat was played by a bot
}

// gameReplayResponse is the JSON returned by the replay endpoint. events is the
// FULL event log, in order, each delta carrying its sequence number; hand_dealt
// deltas include BOTH hands and discarded deltas include BOTH players' cards.
type gameReplayResponse struct {
	GameID string        `json:"game_id"`
	Seats  [2]replaySeat `json:"seats"`
	Winner int           `json:"winner"`
	Target int           `json:"target"`
	Events []Delta       `json:"events"`
}

// handleGameReplay serves the full-visibility replay of one finished game to a
// participant. It mirrors handleGameAnalysis's auth/404 guards: only games in the
// permanent result store are visible (a live game has no Result yet, so it 404s),
// and only a player who holds a seat may read it — an unknown id and a
// non-participant both 404 so the endpoint never reveals a game exists.
// Path: GET /users/me/games/{id}/replay.
func (s *Server) handleGameReplay(w http.ResponseWriter, r *http.Request) {
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

	resp := gameReplayResponse{
		GameID: res.ID,
		Winner: res.Winner,
		// Result does not store the game's score target, so replay reports the
		// standard 121-point game. LIMITATION: a game played to a non-default target
		// (e.g. 61) will be mislabeled here until Result records the target.
		Target: 121,
		Events: projectReplayEvents(res.Events, 0),
	}
	for i := 0; i < 2; i++ {
		resp.Seats[i] = replaySeat{Name: res.Names[i], Bot: res.Bots[i].Name != ""}
	}
	writeJSON(w, http.StatusOK, resp)
}
