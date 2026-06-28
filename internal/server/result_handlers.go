package server

import (
	"log"
	"net/http"
	"time"
)

// recordResult writes a finished game to the permanent result store. It runs on
// the move that ends the game (Apply rejects moves on a finished game, so this
// fires once), and only for games with at least one logged-in player — a
// fully-anonymous guest game has no one to attribute history to. The store is
// idempotent, so a stray double-call is harmless. Must be called under sess.mu.
func (s *Server) recordResult(sess *session) {
	winner, ok := sess.game.Winner()
	if !ok {
		return
	}
	if sess.playerIDs[0] == "" && sess.playerIDs[1] == "" {
		return
	}
	res := Result{
		ID:        sess.id,
		PlayerIDs: sess.playerIDs,
		Names:     sess.names,
		Scores:    sess.game.Scores(),
		Winner:    int(winner),
		Events:    sess.game.Snapshot().Log,
		EndedAt:   s.now(),
	}
	if err := saveResultRetrying(s.results, res, time.Sleep); err != nil {
		log.Printf("save result %s: giving up: %v", sess.id, err)
	}
}

// saveResultRetrying writes a finished game's record, retrying a few times so a
// transient DB blip doesn't lose it. Unlike persist (which the next change
// re-saves), this is a terminal write — the game is reaped from memory afterward,
// so a dropped save is gone forever. SaveResult is idempotent, so retries are safe.
// The success path saves on the first try (no sleep), so callers/tests don't wait.
// sleep is injectable so tests run instantly.
func saveResultRetrying(rs ResultStore, res Result, sleep func(time.Duration)) error {
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		if err = rs.SaveResult(res); err == nil {
			return nil
		}
		if attempt < 5 {
			sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
	}
	return err
}

// gameSummary is one finished game from the requesting player's perspective.
type gameSummary struct {
	ID            string    `json:"id"`
	Opponent      string    `json:"opponent"`
	YourScore     int       `json:"your_score"`
	OpponentScore int       `json:"opponent_score"`
	Won           bool      `json:"won"`
	EndedAt       time.Time `json:"ended_at"`
}

type playerStats struct {
	Total  int `json:"total"`
	Wins   int `json:"wins"`
	Losses int `json:"losses"`
}

type userGamesResponse struct {
	Games []gameSummary `json:"games"`
	Stats playerStats   `json:"stats"`
}

// summarize projects a Result to userID's point of view (you vs opponent).
func summarize(res Result, userID string) gameSummary {
	me := 0
	if res.PlayerIDs[0] != userID {
		me = 1 // ResultsForPlayer guarantees the user is one of the two seats
	}
	opp := 1 - me
	return gameSummary{
		ID:            res.ID,
		Opponent:      res.Names[opp],
		YourScore:     res.Scores[me],
		OpponentScore: res.Scores[opp],
		Won:           res.Winner == me,
		EndedAt:       res.EndedAt,
	}
}

// handleUserGames returns the logged-in user's recent finished games (newest
// first, from their perspective) plus total/win/loss stats.
func (s *Server) handleUserGames(w http.ResponseWriter, r *http.Request) {
	u, ok := s.currentUser(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	results, err := s.results.ResultsForPlayer(u.ID, 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load games")
		return
	}
	total, wins, err := s.results.PlayerStats(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load stats")
		return
	}
	games := make([]gameSummary, 0, len(results))
	for _, res := range results {
		games = append(games, summarize(res, u.ID))
	}
	writeJSON(w, http.StatusOK, userGamesResponse{
		Games: games,
		Stats: playerStats{Total: total, Wins: wins, Losses: total - wins},
	})
}
