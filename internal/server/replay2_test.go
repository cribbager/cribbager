package server

import (
	"bytes"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Tests for GET /games/{id}/replay — the participant-credential replay that
// backs the unified replay+analysis page. Its access matrix must mirror
// GET /games/{id}/analysis exactly (they share finishedGameSubject), and its
// response shape must match the login-only GET /users/me/games/{id}/replay.

// TestGameReplayV2GuestAccess is the NU3 path over the replay half: a pure
// guest (no account, no cookie) plays a live bot game to completion and then
// fetches the full-visibility replay with only the per-game player token —
// a game that never reaches the result store, so the live-session credential
// is the only way in. Mid-game the same request is a 409 (post-game only).
func TestGameReplayV2GuestAccess(t *testing.T) {
	c, rs := newAuthedServer(t) // no signup: the client carries no login session

	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot", Bot: bot.ChampionName})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, data)
	}
	created := decode[createResponse](t, data)
	id, token := created.GameID, created.PlayerToken

	// Unfinished + valid participant token -> 409, not 404: replay is
	// post-game only, but the token already proves participation.
	if r, body := c.do("GET", "/games/"+id+"/replay", token, nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("mid-game replay: %d %s, want 409", r.StatusCode, body)
	}

	playOutGuestGame(t, c, id, token)

	// A fully-guest game must NOT be in the permanent result store — the
	// live-session token path is what serves it.
	if _, ok, _ := rs.ResultByID(id); ok {
		t.Fatal("guest-vs-bot game unexpectedly reached the result store")
	}

	r, body := c.do("GET", "/games/"+id+"/replay", token, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("guest replay: %d %s", r.StatusCode, body)
	}
	got := decode[gameReplayResponse](t, body)
	if got.GameID != id || got.Target != 121 {
		t.Fatalf("game/target = %s/%d, want %s/121", got.GameID, got.Target, id)
	}
	if got.Winner != 0 && got.Winner != 1 {
		t.Errorf("winner = %d, want a seat", got.Winner)
	}
	// The bot seat is flagged; the guest seat is not.
	if got.Seats[0].Bot || !got.Seats[1].Bot {
		t.Errorf("seats = %+v, want seat 0 human, seat 1 bot", got.Seats)
	}
	if len(got.Events) == 0 {
		t.Fatal("no replay events")
	}
	// Full visibility over the token path: every hand_dealt reveals BOTH
	// seats' six, and every discarded delta carries the actual cards.
	for i, e := range got.Events {
		if e.Seq != i+1 {
			t.Errorf("event[%d] seq = %d, want %d", i, e.Seq, i+1)
		}
		switch e.Type {
		case "hand_dealt":
			if len(e.Hands) != 2 || len(e.Hands[0]) != 6 || len(e.Hands[1]) != 6 {
				t.Errorf("event[%d] hand_dealt does not reveal both hands: %+v", i, e)
			}
		case "discarded":
			if len(e.Cards) != 2 {
				t.Errorf("event[%d] discarded does not reveal the cards: %+v", i, e)
			}
		}
	}

	// A token that opens no seat here -> non-revealing 404.
	if r, _ := c.do("GET", "/games/"+id+"/replay", "bogus-token", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("bogus token: %d, want 404", r.StatusCode)
	}
	// No credential at all -> 401.
	if r, _ := c.do("GET", "/games/"+id+"/replay", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("no credential: %d, want 401", r.StatusCode)
	}
}

// TestGameReplayV2LoginAccess covers the login-cookie doors and pins the new
// path's response to the existing login-only endpoint byte-for-byte: a
// participant's happy path, a logged-in spectator (404, never revealing the
// game exists), an unknown id (404), and a registered player's LIVE game
// (404 via the login path — no Result yet — but 409 via the player token).
func TestGameReplayV2LoginAccess(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")

	h0 := mustHand(t, "5H 5S 5C 5D JH KH")
	h1 := mustHand(t, "2C 3C 4C 6C 7C 8C")
	rs.SaveResult(Result{
		ID:        "g1",
		PlayerIDs: [2]string{uid, ""},
		Names:     [2]string{"Alice", "champion"},
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

	// Participant via login -> 200, byte-identical to the /users/me endpoint.
	r, body := c.do("GET", "/games/g1/replay", "", nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("participant: %d %s", r.StatusCode, body)
	}
	got := decode[gameReplayResponse](t, body)
	if got.Seats[0] != (replaySeat{Name: "Alice", Bot: false}) || got.Seats[1] != (replaySeat{Name: "champion", Bot: true}) {
		t.Errorf("seats = %+v, want [{Alice false} {champion true}]", got.Seats)
	}
	if _, old := c.do("GET", "/users/me/games/g1/replay", "", nil); !bytes.Equal(body, old) {
		t.Errorf("participant-credential replay differs from the login-only endpoint:\n%s\nvs\n%s", body, old)
	}

	// A logged-in user who was NOT a participant -> 404 (never reveal).
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c2 := &authedClient{t: t, ts: c.ts, cli: &http.Client{Jar: jar}}
	c2.signup("bob")
	if r, _ := c2.do("GET", "/games/g1/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("spectator: %d, want 404", r.StatusCode)
	}

	// Unknown id -> 404.
	if r, _ := c.do("GET", "/games/nope/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", r.StatusCode)
	}

	// A registered player's LIVE game: via the login path there is no stored
	// Result yet -> 404; via the player token -> 409 until it finishes.
	cr, crData := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	if cr.StatusCode != http.StatusCreated {
		t.Fatalf("create live game: %d %s", cr.StatusCode, crData)
	}
	created := decode[createResponse](t, crData)
	if r, _ := c.do("GET", "/games/"+created.GameID+"/replay", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("live game via login: %d, want 404", r.StatusCode)
	}
	if r, _ := c.do("GET", "/games/"+created.GameID+"/replay", created.PlayerToken, nil); r.StatusCode != http.StatusConflict {
		t.Errorf("live game via token: %d, want 409", r.StatusCode)
	}
}
