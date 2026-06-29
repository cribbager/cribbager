package server

import (
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// mustHand parses space-separated two-char card notation ("5H 5S ...").
func mustHand(t *testing.T, s string) []cribbage.Card {
	t.Helper()
	fields := strings.Fields(s)
	out := make([]cribbage.Card, len(fields))
	for i, f := range fields {
		c, err := cribbage.ParseCard(f)
		if err != nil {
			t.Fatalf("parse %q: %v", f, err)
		}
		out[i] = c
	}
	return out
}

func hand6(t *testing.T, s string) [6]cribbage.Card {
	t.Helper()
	cards := mustHand(t, s)
	if len(cards) != 6 {
		t.Fatalf("want 6 cards, got %d", len(cards))
	}
	var h [6]cribbage.Card
	copy(h[:], cards)
	return h
}

// TestAnalyzeDiscardsVerdicts checks the core engine: an optimal discard is
// flagged optimal with zero EV lost, a sub-optimal one carries the right positive
// delta (matching eval's own ranking), and the summary aggregates correctly. It
// also exercises both crib owners (dealer vs pone), since the crib sign flips the EV.
func TestAnalyzeDiscardsVerdicts(t *testing.T) {
	// Hand 1: seat 0 is dealer (own crib). Feed the engine's own best discard.
	h1 := hand6(t, "5H 5S 5C 5D JH KH")
	bestThrow, _ := eval.BestDiscardEV(h1, true)

	// Hand 2: seat 0 is pone (dealer is seat 1, opponent's crib). Feed the
	// worst-ranked hold so the delta is clearly positive.
	h2 := hand6(t, "AH 2H 3H 4H 9D TD")
	ranked2 := eval.RankDiscards(h2, false)
	worst := ranked2[len(ranked2)-1]
	wantDelta := round4(ranked2[0].Score - worst.Score)

	res := Result{
		ID:        "g-analyze",
		PlayerIDs: [2]string{"u1", "u2"},
		Names:     [2]string{"Alice", "Bob"},
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h1[:], mustHand(t, "2C 3C 4C 6C 7C 8C")}},
			game.Discarded{Seat: game.Seat0, Cards: bestThrow},
			game.Discarded{Seat: game.Seat1, Cards: [2]cribbage.Card{mustHand(t, "2C")[0], mustHand(t, "3C")[0]}},
			game.HandDealt{Dealer: game.Seat1, Hands: [2][]cribbage.Card{h2[:], mustHand(t, "AC 4C 5C 6C 7C 8C")}},
			game.Discarded{Seat: game.Seat0, Cards: worst.Discard},
		},
		EndedAt: time.Now(),
	}

	got := analyzeDiscards(res, game.Seat0)

	if len(got.Discards) != 2 {
		t.Fatalf("discards = %d, want 2", len(got.Discards))
	}

	d1 := got.Discards[0]
	if !d1.Optimal {
		t.Errorf("hand 1 should be optimal (fed the engine's best throw): %+v", d1)
	}
	if d1.DeltaEV != 0 {
		t.Errorf("hand 1 delta = %v, want 0", d1.DeltaEV)
	}
	if !d1.Dealer {
		t.Errorf("hand 1 dealer flag = false, want true (seat 0 dealt)")
	}
	if !samePair(d1.Throw, bestThrow) || !samePair(d1.BestThrow, bestThrow) {
		t.Errorf("hand 1 throw/best mismatch: throw=%v best=%v want=%v", d1.Throw, d1.BestThrow, bestThrow)
	}

	d2 := got.Discards[1]
	if d2.Optimal {
		t.Errorf("hand 2 should be sub-optimal (fed the worst hold): %+v", d2)
	}
	if d2.Dealer {
		t.Errorf("hand 2 dealer flag = true, want false (seat 1 dealt)")
	}
	if d2.DeltaEV != wantDelta {
		t.Errorf("hand 2 delta = %v, want %v", d2.DeltaEV, wantDelta)
	}
	if d2.BestEV < d2.KeepEV {
		t.Errorf("best EV %v should be >= chosen EV %v", d2.BestEV, d2.KeepEV)
	}
	if math.Abs((d2.BestEV-d2.KeepEV)-d2.DeltaEV) > 1e-4 {
		t.Errorf("delta %v != best-keep (%v-%v)", d2.DeltaEV, d2.BestEV, d2.KeepEV)
	}

	if got.Summary.Hands != 2 || got.Summary.OptimalDiscards != 1 {
		t.Errorf("summary hands/optimal = %d/%d, want 2/1", got.Summary.Hands, got.Summary.OptimalDiscards)
	}
	if got.Summary.TotalEVLost != wantDelta {
		t.Errorf("summary total EV lost = %v, want %v", got.Summary.TotalEVLost, wantDelta)
	}

	// Same game from seat 1's perspective sees only its own (single) discard.
	if s1 := analyzeDiscards(res, game.Seat1); s1.Summary.Hands != 1 || s1.Seat != 1 {
		t.Errorf("seat 1 view: hands=%d seat=%d, want 1/1", s1.Summary.Hands, s1.Seat)
	}
}

// TestGameAnalysisEndpoint covers auth, the post-game-only gate (a live game has
// no stored Result, so it 404s), the participant restriction, and the JSON shape.
func TestGameAnalysisEndpoint(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")

	h := hand6(t, "5H 5S 5C 5D JH KH")
	throw, _ := eval.BestDiscardEV(h, true)
	rs.SaveResult(Result{
		ID:        "g1",
		PlayerIDs: [2]string{uid, "u2"},
		Names:     [2]string{"Alice", "Bob"},
		Scores:    [2]int{121, 90},
		Winner:    0,
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h[:], mustHand(t, "2C 3C 4C 6C 7C 8C")}},
			game.Discarded{Seat: game.Seat0, Cards: throw},
		},
		EndedAt: time.Now(),
	})
	// A finished game alice is NOT in (participant restriction -> 404, not 403/leak).
	rs.SaveResult(Result{ID: "other", PlayerIDs: [2]string{"u2", "u3"}, Names: [2]string{"Bob", "Cara"}, EndedAt: time.Now()})

	// Happy path: 200 with the analysis.
	resp, data := c.do("GET", "/users/me/games/g1/analysis", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("analysis: %d %s", resp.StatusCode, data)
	}
	got := decode[gameAnalysisResponse](t, data)
	if got.GameID != "g1" || got.Seat != 0 {
		t.Errorf("game/seat = %s/%d, want g1/0", got.GameID, got.Seat)
	}
	if len(got.Discards) != 1 || !got.Discards[0].Optimal {
		t.Errorf("discards = %+v, want 1 optimal", got.Discards)
	}
	if got.Summary != (analysisSummary{Hands: 1, OptimalDiscards: 1, TotalEVLost: 0}) {
		t.Errorf("summary = %+v, want 1/1/0", got.Summary)
	}

	// Unknown id -> 404.
	if r, _ := c.do("GET", "/users/me/games/nope/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", r.StatusCode)
	}
	// A game the user isn't in -> 404.
	if r, _ := c.do("GET", "/users/me/games/other/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("non-participant: %d, want 404", r.StatusCode)
	}

	// A live, in-progress game has no stored Result -> 404 (post-game only).
	cr, crData := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	if cr.StatusCode != http.StatusCreated {
		t.Fatalf("create live game: %d %s", cr.StatusCode, crData)
	}
	liveID := decode[createResponse](t, crData).GameID
	if r, _ := c.do("GET", "/users/me/games/"+liveID+"/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("live game analysis: %d, want 404 (post-game only)", r.StatusCode)
	}

	// Unauthenticated -> 401.
	plain := &http.Client{}
	req, _ := http.NewRequest("GET", c.ts.URL+"/users/me/games/g1/analysis", nil)
	r2, _ := plain.Do(req)
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("guest analysis: %d, want 401", r2.StatusCode)
	}
	r2.Body.Close()
}
