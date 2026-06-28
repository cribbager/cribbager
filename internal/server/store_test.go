package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// clientFor wraps a given Server in an httptest server + testClient, so a test can
// inject a Store and later spin up a second server over the same Store.
func clientFor(t *testing.T, srv *Server) *testClient {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &testClient{t: t, ts: ts}
}

// TestStorePersistsAndRestoresGame: a game created and played on one server is
// recovered identically by a fresh server sharing the same Store (the restart
// path), and remains playable.
func TestStorePersistsAndRestoresGame(t *testing.T) {
	store := NewMemStore()

	// Server 1: open game, joined, host discards — all write-through to the store.
	srv1 := New()
	srv1.SetStore(store)
	c1 := clientFor(t, srv1)
	_, data := c1.do("POST", "/games", "", createRequest{Mode: "open", Name: "Alice"})
	created := decode[createResponse](t, data)
	id, hostTok := created.GameID, created.PlayerToken
	_, jd := c1.do("POST", "/games/"+id+"/join", "", joinRequest{Name: "Bob"})
	guestTok := decode[joinResponse](t, jd).PlayerToken
	_, hv := c1.do("GET", "/games/"+id, hostTok, nil)
	hostHand := decode[game.PlayerView](t, hv).YourHand
	c1.do("POST", "/games/"+id+"/actions", hostTok, actionRequest{Type: "discard", Cards: []cribbage.Card{hostHand[0], hostHand[1]}})
	_, hv2 := c1.do("GET", "/games/"+id, hostTok, nil)
	before := decode[game.PlayerView](t, hv2)

	// Server 2: a fresh process sharing the same store, restored at boot.
	srv2 := New()
	srv2.SetStore(store)
	if err := srv2.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	c2 := clientFor(t, srv2)

	// The game is back: the host's token still authenticates and the view matches.
	resp, av := c2.do("GET", "/games/"+id, hostTok, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("restored game GET: %d %s", resp.StatusCode, av)
	}
	if after := decode[game.PlayerView](t, av); !reflect.DeepEqual(before, after) {
		t.Fatalf("restored view mismatch:\n before %+v\n after  %+v", before, after)
	}

	// And it's playable on the new server: the guest discards, triggering the cut
	// — which relies on the restored undealt remainder (the pending starter).
	_, gv := c2.do("GET", "/games/"+id, guestTok, nil)
	gh := decode[game.PlayerView](t, gv).YourHand
	resp2, body2 := c2.do("POST", "/games/"+id+"/actions", guestTok, actionRequest{Type: "discard", Cards: []cribbage.Card{gh[0], gh[1]}})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("guest discard on restored server: %d %s", resp2.StatusCode, body2)
	}
	if v := decode[game.PlayerView](t, mustGet(c2, id, hostTok)).Phase; v != game.PhasePlay {
		t.Fatalf("after both discard on restored game, phase = %v, want play", v)
	}
}

// TestStoreRestoresBotGame: a vs-bot game restores with the bot seat re-attached,
// so it stays playable (the bot responds to the human's move on the new server).
func TestStoreRestoresBotGame(t *testing.T) {
	store := NewMemStore()
	srv1 := New()
	srv1.SetStore(store)
	c1 := clientFor(t, srv1)
	_, data := c1.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	id, tok := created.GameID, created.PlayerToken

	srv2 := New()
	srv2.SetStore(store)
	if err := srv2.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	c2 := clientFor(t, srv2)

	hand := decode[game.PlayerView](t, mustGet(c2, id, tok)).YourHand
	if len(hand) != 6 {
		t.Fatalf("restored bot game hand = %d, want 6", len(hand))
	}
	resp, body := c2.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "discard", Cards: []cribbage.Card{hand[0], hand[1]}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("discard vs restored bot: %d %s", resp.StatusCode, body)
	}
	// The re-attached bot must have discarded too, advancing past the cut to play.
	if v := decode[game.PlayerView](t, mustGet(c2, id, tok)).Phase; v != game.PhasePlay {
		t.Fatalf("after discard vs restored bot, phase = %v, want play (bot didn't act?)", v)
	}
}

func mustGet(c *testClient, id, tok string) []byte {
	c.t.Helper()
	resp, data := c.do("GET", "/games/"+id, tok, nil)
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("GET /games/%s: %d %s", id, resp.StatusCode, data)
	}
	return data
}
