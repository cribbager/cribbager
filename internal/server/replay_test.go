package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestProjectReplayEventFullVisibility is the core unit check: the replay
// projection reveals what the live (per-seat) projection redacts — BOTH seats'
// dealt hands on hand_dealt and BOTH players' discards — while keeping every
// other event byte-identical to the live delta, with sequence numbers in order.
func TestProjectReplayEventFullVisibility(t *testing.T) {
	h0 := mustHand(t, "5H 5S 5C 5D JH KH")
	h1 := mustHand(t, "2C 3C 4C 6C 7C 8C")
	events := []game.Event{
		game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h0, h1}},
		game.Discarded{Seat: game.Seat0, Cards: [2]cribbage.Card{h0[4], h0[5]}},
		game.Discarded{Seat: game.Seat1, Cards: [2]cribbage.Card{h1[4], h1[5]}},
		game.StarterCut{Card: mustHand(t, "9D")[0], Heels: 0},
		game.GameWon{Seat: game.Seat1},
	}

	deltas := projectReplayEvents(events, 0)
	if len(deltas) != len(events) {
		t.Fatalf("deltas = %d, want %d", len(deltas), len(events))
	}
	// seq is complete and ordered: 1..N.
	for i, d := range deltas {
		if d.Seq != i+1 {
			t.Errorf("delta[%d] seq = %d, want %d", i, d.Seq, i+1)
		}
	}

	// hand_dealt: BOTH full hands present (the live projection would redact one).
	hd := deltas[0]
	if hd.Type != "hand_dealt" {
		t.Fatalf("delta[0] type = %q, want hand_dealt", hd.Type)
	}
	if len(hd.Hands) != 2 {
		t.Fatalf("hand_dealt Hands len = %d, want 2", len(hd.Hands))
	}
	if !cardsEqual(hd.Hands[0], h0) || !cardsEqual(hd.Hands[1], h1) {
		t.Errorf("hand_dealt hands = %v / %v, want %v / %v", hd.Hands[0], hd.Hands[1], h0, h1)
	}
	if hd.Hand != nil {
		t.Errorf("hand_dealt should not set the per-seat Hand field, got %v", hd.Hand)
	}

	// discarded: BOTH seats' actual cards present (live redacts the opponent's).
	d0, d1 := deltas[1], deltas[2]
	if d0.Type != "discarded" || d0.Seat == nil || *d0.Seat != game.Seat0 {
		t.Fatalf("delta[1] = %+v, want discarded seat 0", d0)
	}
	if !cardsEqual(d0.Cards, []cribbage.Card{h0[4], h0[5]}) {
		t.Errorf("seat 0 discard cards = %v, want %v", d0.Cards, []cribbage.Card{h0[4], h0[5]})
	}
	if d1.Seat == nil || *d1.Seat != game.Seat1 || !cardsEqual(d1.Cards, []cribbage.Card{h1[4], h1[5]}) {
		t.Errorf("seat 1 discard = %+v, want seat 1 cards %v", d1, []cribbage.Card{h1[4], h1[5]})
	}

	// Other events delegate to the live projection, so they stay identical.
	if sc := deltas[3]; sc.Type != "starter_cut" || sc.Card == nil || *sc.Card != mustHand(t, "9D")[0] {
		t.Errorf("starter_cut delta = %+v, want starter_cut of 9D", sc)
	}
	if gw := deltas[4]; gw.Type != "game_won" || gw.Seat == nil || *gw.Seat != game.Seat1 {
		t.Errorf("game_won delta = %+v, want game_won seat 1", gw)
	}
}

// TestProjectEventStillRedacts guards the safety invariant: the LIVE projection
// is untouched — it still redacts the opponent's hand and discards, and never
// populates the replay-only Hands field.
func TestProjectEventStillRedacts(t *testing.T) {
	h0 := mustHand(t, "5H 5S 5C 5D JH KH")
	h1 := mustHand(t, "2C 3C 4C 6C 7C 8C")

	hd := projectEvent(game.Seat0, game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h0, h1}}, 1)
	if hd.Hands != nil {
		t.Errorf("live hand_dealt must not set Hands (replay-only), got %v", hd.Hands)
	}
	if !cardsEqual(hd.Hand, h0) || hd.OpponentCards != 6 {
		t.Errorf("live hand_dealt should reveal only viewer hand + opp count, got hand=%v opp=%d", hd.Hand, hd.OpponentCards)
	}

	dOpp := projectEvent(game.Seat0, game.Discarded{Seat: game.Seat1, Cards: [2]cribbage.Card{h1[4], h1[5]}}, 2)
	if dOpp.Cards != nil {
		t.Errorf("live projection must redact opponent discard, got %v", dOpp.Cards)
	}
}

// TestGameReplayEndpoint covers auth, the post-game-only gate, the participant
// restriction, the JSON response shape, and the full-visibility bypass over the
// wire (both hands + both discards reach a participant).
func TestGameReplayEndpoint(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")

	h0 := mustHand(t, "5H 5S 5C 5D JH KH")
	h1 := mustHand(t, "2C 3C 4C 6C 7C 8C")
	rs.SaveResult(Result{
		ID:        "g1",
		PlayerIDs: [2]string{uid, ""},
		Names:     [2]string{"Alice", "champion"},
		Scores:    [2]int{121, 90},
		Winner:    0,
		Bots:      [2]BotInfo{{}, {Name: "champion", Version: "v2"}},
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h0, h1}},
			game.Discarded{Seat: game.Seat0, Cards: [2]cribbage.Card{h0[4], h0[5]}},
			game.Discarded{Seat: game.Seat1, Cards: [2]cribbage.Card{h1[4], h1[5]}},
			game.GameWon{Seat: game.Seat0},
		},
		EndedAt: time.Now(),
	})
	// A finished game alice is NOT in (participant restriction -> 404, not a leak).
	rs.SaveResult(Result{ID: "other", PlayerIDs: [2]string{"u2", "u3"}, Names: [2]string{"Bob", "Cara"}, EndedAt: time.Now()})

	// Happy path: 200 with the full-visibility replay.
	resp, data := c.do("GET", "/users/me/games/g1/replay", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replay: %d %s", resp.StatusCode, data)
	}
	got := decode[gameReplayResponse](t, data)
	if got.GameID != "g1" || got.Winner != 0 || got.Target != 121 {
		t.Errorf("game/winner/target = %s/%d/%d, want g1/0/121", got.GameID, got.Winner, got.Target)
	}
	if got.Seats[0] != (replaySeat{Name: "Alice", Bot: false}) || got.Seats[1] != (replaySeat{Name: "champion", Bot: true}) {
		t.Errorf("seats = %+v, want [{Alice false} {champion true}]", got.Seats)
	}

	// Event log is complete and ordered.
	if len(got.Events) != 4 {
		t.Fatalf("events = %d, want 4", len(got.Events))
	}
	for i, e := range got.Events {
		if e.Seq != i+1 {
			t.Errorf("event[%d] seq = %d, want %d", i, e.Seq, i+1)
		}
	}

	// The redaction bypass: BOTH dealt hands present on hand_dealt...
	hd := got.Events[0]
	if hd.Type != "hand_dealt" || len(hd.Hands) != 2 ||
		!cardsEqual(hd.Hands[0], h0) || !cardsEqual(hd.Hands[1], h1) {
		t.Errorf("hand_dealt did not reveal both hands: %+v", hd)
	}
	// ...and BOTH players' discards, including the opponent's (seat 1).
	if d := got.Events[2]; d.Type != "discarded" || d.Seat == nil || *d.Seat != game.Seat1 ||
		!cardsEqual(d.Cards, []cribbage.Card{h1[4], h1[5]}) {
		t.Errorf("opponent discard not revealed: %+v", d)
	}

	// Unknown id -> 404.
	if r, _ := c.do("GET", "/users/me/games/nope/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", r.StatusCode)
	}
	// A game the user isn't in -> 404 (never reveal existence).
	if r, _ := c.do("GET", "/users/me/games/other/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("non-participant: %d, want 404", r.StatusCode)
	}

	// A live, in-progress game has no stored Result -> 404 (post-game only).
	cr, crData := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	if cr.StatusCode != http.StatusCreated {
		t.Fatalf("create live game: %d %s", cr.StatusCode, crData)
	}
	liveID := decode[createResponse](t, crData).GameID
	if r, _ := c.do("GET", "/users/me/games/"+liveID+"/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("live game replay: %d, want 404 (post-game only)", r.StatusCode)
	}

	// Unauthenticated -> 401.
	plain := &http.Client{}
	req, _ := http.NewRequest("GET", c.ts.URL+"/users/me/games/g1/replay", nil)
	r2, _ := plain.Do(req)
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("guest replay: %d, want 401", r2.StatusCode)
	}
	r2.Body.Close()
}

// cardsEqual reports whether two card slices are equal in order.
func cardsEqual(a, b []cribbage.Card) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
