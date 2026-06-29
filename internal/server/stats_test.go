package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
	scorehand "github.com/cribbager/cribbager/internal/scoring/hand"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// peg/show build minimal scoring events: gamePoints only reads each event's
// Score.Total, so setting Total alone is enough to assert known per-category points.
func pegScore(total int) pegging.Result { return pegging.Result{Total: total} }
func showScore(total int) scorehand.Result {
	return scorehand.Result{Total: total}
}

// statsGames returns three finished games for player "u1", each with a known
// per-category point split, plus one game u1 is not in (must be ignored):
//
//	g1: u1 seat0 + dealer -> pegging 5 (2 play + 1 go + 2 heels), hand 8, crib 4
//	g2: u1 seat1 + dealer -> pegging 4 (3 play + 1 go),           hand 12, crib 6
//	g3: u1 seat0, pone     -> pegging 0, hand 0, crib 0 (heels/crib go to dealer)
//	g4: no u1 -> skipped
func statsGames(playerID string) []Result {
	c := cribbage.Card{Rank: 5, Suit: cribbage.Hearts} // any card; only event Score totals matter here
	g1 := Result{
		ID: "g1", PlayerIDs: [2]string{playerID, "u2"}, Names: [2]string{"U1", "U2"},
		Scores: [2]int{121, 80}, Winner: 0, EndedAt: time.Now(),
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat0},
			game.StarterCut{Card: c, Heels: 2}, // dealer = seat0 = u1
			game.CardPlayed{Seat: game.Seat0, Card: c, Score: pegScore(2)},
			game.CardPlayed{Seat: game.Seat1, Card: c, Score: pegScore(7)}, // opponent: ignored
			game.GoAwarded{Seat: game.Seat0, Points: 1},
			game.HandShown{Seat: game.Seat0, Score: showScore(8)},
			game.HandShown{Seat: game.Seat1, Score: showScore(20)}, // opponent: ignored
			game.CribShown{Score: showScore(4)},                    // dealer = seat0 = u1
		},
	}
	g2 := Result{
		ID: "g2", PlayerIDs: [2]string{"u3", playerID}, Names: [2]string{"U3", "U1"},
		Scores: [2]int{90, 121}, Winner: 1, EndedAt: time.Now(),
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat1}, // u1 is dealer
			game.StarterCut{Card: c, Heels: 0},
			game.CardPlayed{Seat: game.Seat1, Card: c, Score: pegScore(3)},
			game.GoAwarded{Seat: game.Seat1, Points: 1},
			game.HandShown{Seat: game.Seat1, Score: showScore(12)},
			game.CribShown{Score: showScore(6)}, // dealer = seat1 = u1
		},
	}
	g3 := Result{
		ID: "g3", PlayerIDs: [2]string{playerID, "u2"}, Names: [2]string{"U1", "U2"},
		Scores: [2]int{60, 121}, Winner: 1, EndedAt: time.Now(),
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat1}, // u1 (seat0) is pone
			game.StarterCut{Card: c, Heels: 2}, // heels to dealer seat1, not u1
			game.HandShown{Seat: game.Seat1, Score: showScore(9)},
			game.CribShown{Score: showScore(5)}, // dealer seat1, not u1
		},
	}
	g4 := Result{
		ID: "g4", PlayerIDs: [2]string{"u2", "u3"}, Names: [2]string{"U2", "U3"},
		Scores: [2]int{121, 100}, Winner: 0, EndedAt: time.Now(),
		Events: []game.Event{game.HandShown{Seat: game.Seat0, Score: showScore(99)}},
	}
	return []Result{g1, g2, g3, g4}
}

func TestComputePointStats(t *testing.T) {
	got := computePointStats(statsGames("u1"), "u1")

	if got.Games != 3 {
		t.Fatalf("games = %d, want 3 (g4 excluded)", got.Games)
	}
	// pegging per game: [5, 4, 0]
	if want := (categoryStats{Min: 0, Max: 5, Avg: 3, Total: 9}); got.Pegging != want {
		t.Errorf("pegging = %+v, want %+v", got.Pegging, want)
	}
	// hand per game: [8, 12, 0] -> avg 20/3 = 6.67
	if want := (categoryStats{Min: 0, Max: 12, Avg: 6.67, Total: 20}); got.Hand != want {
		t.Errorf("hand = %+v, want %+v", got.Hand, want)
	}
	// crib per game: [4, 6, 0] -> avg 10/3 = 3.33
	if want := (categoryStats{Min: 0, Max: 6, Avg: 3.33, Total: 10}); got.Crib != want {
		t.Errorf("crib = %+v, want %+v", got.Crib, want)
	}
}

// TestGamePointsSumsToScore checks the invariant that pegging+hand+crib equals
// the seat's final score for a game whose every point is logged.
func TestGamePointsSumsToScore(t *testing.T) {
	g := statsGames("u1")[0] // g1: u1 seat0
	p, h, c := gamePoints(g, game.Seat0)
	if p+h+c != 5+8+4 {
		t.Errorf("g1 seat0 points = %d+%d+%d, want 17", p, h, c)
	}
	// Opponent (seat1) in g1: play 7, hand 20, no crib/heels (pone) = 27.
	p, h, c = gamePoints(g, game.Seat1)
	if p != 7 || h != 20 || c != 0 {
		t.Errorf("g1 seat1 points = peg %d hand %d crib %d, want 7/20/0", p, h, c)
	}
}

func TestComputePointStatsZeroGames(t *testing.T) {
	got := computePointStats(nil, "nobody")
	if got != (pointStats{}) {
		t.Errorf("zero-games stats = %+v, want all zero", got)
	}
}

func TestUserStatsEndpoint(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")
	for _, r := range statsGames(uid) {
		rs.SaveResult(r)
	}

	resp, data := c.do("GET", "/users/me/stats", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: %d %s", resp.StatusCode, data)
	}
	got := decode[pointStats](t, data)
	if got.Games != 3 {
		t.Fatalf("games = %d, want 3", got.Games)
	}
	if want := (categoryStats{Min: 0, Max: 5, Avg: 3, Total: 9}); got.Pegging != want {
		t.Errorf("pegging = %+v, want %+v", got.Pegging, want)
	}
	if want := (categoryStats{Min: 0, Max: 12, Avg: 6.67, Total: 20}); got.Hand != want {
		t.Errorf("hand = %+v, want %+v", got.Hand, want)
	}
	if want := (categoryStats{Min: 0, Max: 6, Avg: 3.33, Total: 10}); got.Crib != want {
		t.Errorf("crib = %+v, want %+v", got.Crib, want)
	}

	// A brand-new user with no finished games gets a clean all-zero payload.
	// signup logs in via the shared cookie jar, so the session is now this user.
	c.signup("bob")
	resp, data = c.do("GET", "/users/me/stats", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("zero-games stats: %d %s", resp.StatusCode, data)
	}
	if z := decode[pointStats](t, data); z != (pointStats{}) {
		t.Errorf("new user stats = %+v, want all zero", z)
	}

	// Unauthenticated -> 401.
	plain := &http.Client{}
	req, _ := http.NewRequest("GET", c.ts.URL+"/users/me/stats", nil)
	r2, _ := plain.Do(req)
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("guest /users/me/stats: %d, want 401", r2.StatusCode)
	}
	r2.Body.Close()
}
