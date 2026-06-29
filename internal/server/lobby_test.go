package server

import (
	"net/http"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// lobbyIDs returns the set of game ids currently listed in GET /lobby, asserting
// the response is well-formed (200 + a non-null games array).
func (c *testClient) lobbyIDs(t *testing.T) map[string]lobbyGame {
	t.Helper()
	resp, data := c.do("GET", "/lobby", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /lobby: %d %s", resp.StatusCode, data)
	}
	lr := decode[lobbyResponse](t, data)
	if lr.Games == nil {
		t.Fatalf("lobby games is null, want [] (body: %s)", data)
	}
	out := map[string]lobbyGame{}
	for _, g := range lr.Games {
		out[g.GameID] = g
	}
	return out
}

func (c *testClient) create(t *testing.T, req createRequest) createResponse {
	t.Helper()
	resp, data := c.do("POST", "/games", "", req)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %+v: %d %s", req, resp.StatusCode, data)
	}
	return decode[createResponse](t, data)
}

// TestLobbyListsOnlyPublicOpenGames is the core matrix: a public open game is
// listed; a private (default) open game, a bot game, and an explicitly
// public=false open game are not.
func TestLobbyListsOnlyPublicOpenGames(t *testing.T) {
	c := newTestClient(t)

	pub := c.create(t, createRequest{Mode: "open", Public: true, Name: "Ada"})
	priv := c.create(t, createRequest{Mode: "open", Name: "Grace"})             // default: private
	privExplicit := c.create(t, createRequest{Mode: "open", Public: false})     // explicit private
	botGame := c.create(t, createRequest{Mode: "bot", Public: true, Name: "x"}) // public ignored for bots

	listed := c.lobbyIDs(t)

	if _, ok := listed[pub.GameID]; !ok {
		t.Errorf("public open game %s not listed", pub.GameID)
	}
	if _, ok := listed[priv.GameID]; ok {
		t.Errorf("private (default) open game %s should not be listed", priv.GameID)
	}
	if _, ok := listed[privExplicit.GameID]; ok {
		t.Errorf("explicit private open game %s should not be listed", privExplicit.GameID)
	}
	if _, ok := listed[botGame.GameID]; ok {
		t.Errorf("bot game %s should not be listed", botGame.GameID)
	}

	// JSON shape of the listed public game.
	entry := listed[pub.GameID]
	if entry.HostName != "Ada" {
		t.Errorf("host_name = %q, want Ada", entry.HostName)
	}
	if entry.OpenSeat != game.Seat1 {
		t.Errorf("open_seat = %d, want %d", entry.OpenSeat, game.Seat1)
	}
	if entry.CreatedAt.IsZero() {
		t.Errorf("created_at is zero, want a timestamp")
	}
}

// TestLobbyDropsGameOnceJoined checks a public game leaves the lobby as soon as
// its open seat is claimed (full/started).
func TestLobbyDropsGameOnceJoined(t *testing.T) {
	c := newTestClient(t)
	pub := c.create(t, createRequest{Mode: "open", Public: true, Name: "host"})

	if _, ok := c.lobbyIDs(t)[pub.GameID]; !ok {
		t.Fatalf("public game not listed before join")
	}

	resp, data := c.do("POST", "/games/"+pub.GameID+"/join", "", joinRequest{Name: "guest"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %s", resp.StatusCode, data)
	}

	if _, ok := c.lobbyIDs(t)[pub.GameID]; ok {
		t.Errorf("joined (full) game %s should drop off the lobby", pub.GameID)
	}
}

// TestLobbyDropsGameOnAbandon checks an abandoned public game leaves the lobby.
func TestLobbyDropsGameOnAbandon(t *testing.T) {
	c := newTestClient(t)
	pub := c.create(t, createRequest{Mode: "open", Public: true, Name: "host"})

	if _, ok := c.lobbyIDs(t)[pub.GameID]; !ok {
		t.Fatalf("public game not listed before abandon")
	}

	resp, data := c.do("POST", "/games/"+pub.GameID+"/abandon", pub.PlayerToken, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("abandon: %d %s", resp.StatusCode, data)
	}

	if _, ok := c.lobbyIDs(t)[pub.GameID]; ok {
		t.Errorf("abandoned game %s should drop off the lobby", pub.GameID)
	}
}

// TestLobbyExcludesFinishedGame plays a public open game to completion between two
// humans and checks it is absent from the lobby (a finished game's seat is, by
// definition, claimed — so it also fails the open-seat filter).
func TestLobbyExcludesFinishedGame(t *testing.T) {
	c := newTestClient(t)
	host := c.create(t, createRequest{Mode: "open", Public: true, Name: "host"})

	resp, data := c.do("POST", "/games/"+host.GameID+"/join", "", joinRequest{Name: "guest"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %s", resp.StatusCode, data)
	}
	guest := decode[joinResponse](t, data)

	tokens := map[game.Seat]string{game.Seat0: host.PlayerToken, game.Seat1: guest.PlayerToken}
	id := host.GameID

	finished := false
	for step := 0; step < 20000 && !finished; step++ {
		// Drive whichever seat owes an action, reading each seat's own view.
		for seat := game.Seat0; seat < 2; seat++ {
			_, body := c.do("GET", "/games/"+id, tokens[seat], nil)
			v := decode[game.PlayerView](t, body)
			if v.Winner != nil {
				finished = true
				break
			}
			switch v.Phase {
			case game.PhaseDiscard:
				if len(v.YourHand) == 6 {
					act := actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}}
					c.do("POST", "/games/"+id+"/actions", tokens[seat], act)
				}
			case game.PhasePlay:
				if v.ToPlay != nil && *v.ToPlay == v.You && len(v.LegalPlays) > 0 {
					card := v.LegalPlays[0]
					c.do("POST", "/games/"+id+"/actions", tokens[seat], actionRequest{Type: "play", Card: &card})
				}
			}
		}
	}
	if !finished {
		t.Fatal("game did not finish")
	}

	if _, ok := c.lobbyIDs(t)[id]; ok {
		t.Errorf("finished game %s should not be listed", id)
	}
}

// TestLobbyEmptyIsEmptyArray checks an empty lobby serializes as [] (never null),
// so the frontend can iterate unconditionally.
func TestLobbyEmptyIsEmptyArray(t *testing.T) {
	c := newTestClient(t)
	resp, data := c.do("GET", "/lobby", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /lobby: %d %s", resp.StatusCode, data)
	}
	if string(data) != `{"games":[]}`+"\n" {
		t.Errorf("empty lobby body = %q, want {\"games\":[]}", data)
	}
}
