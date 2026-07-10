package server

import (
	"net/http"

	"github.com/cribbager/cribbager/internal/game"
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
	writeJSON(w, http.StatusOK, replayResponseFor(res.ID, participantGameFromResult(res, 0)))
}

// handleGameReplayV2 serves the SAME full-visibility replay to ANY participant
// of a finished game — including guests — over the same two credentials the v2
// analysis endpoint accepts (per-game player token as Bearer, or the login
// cookie) with the same 401/404/409 semantics: they share
// finishedGameSubject, so the unified replay+analysis page can fetch both
// halves with one credential. The response shape is identical to
// GET /users/me/games/{id}/replay. Path: GET /games/{id}/replay.
func (s *Server) handleGameReplayV2(w http.ResponseWriter, r *http.Request) {
	pg, ok := s.finishedGameSubject(w, r)
	if !ok {
		return // finishedGameSubject wrote the error
	}
	writeJSON(w, http.StatusOK, replayResponseFor(r.PathValue("id"), pg))
}

// replayResponseFor assembles the wire replay for a resolved finished game.
// Both replay endpoints build their response here, so the login-only and
// participant-credential paths can never drift in shape. The requester's seat
// is irrelevant: a replay shows the whole game.
func replayResponseFor(id string, pg participantGame) gameReplayResponse {
	resp := gameReplayResponse{
		GameID: id,
		Winner: pg.winner,
		// Neither the session nor the Result records the game's score target, so
		// replay reports the standard 121-point game. LIMITATION: a game played to
		// a non-default target (e.g. 61) will be mislabeled here until the target
		// is recorded.
		Target: 121,
		Events: projectReplayEvents(pg.events, 0),
	}
	for seat := game.Seat0; seat < 2; seat++ {
		resp.Seats[seat] = replaySeat{Name: pg.names[seat], Bot: pg.bots[seat]}
	}
	return resp
}
