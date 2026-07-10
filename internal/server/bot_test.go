package server

import (
	"net/http"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestBotsEndpoint checks GET /bots advertises the production bots and the
// default, so a client can populate a bot picker or validate a --bot flag.
func TestBotsEndpoint(t *testing.T) {
	c := newTestClient(t)
	resp, data := c.do("GET", "/bots", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /bots: %d %s", resp.StatusCode, data)
	}
	got := decode[botsResponse](t, data)
	if got.Default != bot.DefaultName {
		t.Errorf("default = %q, want %q", got.Default, bot.DefaultName)
	}
	has := map[string]bool{}
	for _, n := range got.Bots {
		has[n] = true
	}
	for _, want := range []string{"champion", "random"} {
		if !has[want] {
			t.Errorf("GET /bots is missing %q (got %v)", want, got.Bots)
		}
	}
}

// TestCreateUnknownBotRejected checks that selecting a bot name that isn't a
// production bot is a clean 400 (not a 500 or a silently-seated default).
func TestCreateUnknownBotRejected(t *testing.T) {
	c := newTestClient(t)
	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot", Bot: "nope"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create with unknown bot: %d %s, want 400", resp.StatusCode, data)
	}
	// A lab challenger name must be rejected here too — lab bots are never seatable.
	resp, _ = c.do("POST", "/games", "", createRequest{Mode: "bot", Bot: "candidate"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create with a lab challenger name: %d, want 400", resp.StatusCode)
	}
}

// TestCreateSelectedBotPersisted plays a logged-in game against an explicitly
// selected NON-default bot and checks the finished game records that bot's actual
// name and version — proving the recorded identity flows from the seated instance,
// not a hardcoded champion constant.
func TestCreateSelectedBotPersisted(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("erin")

	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot", Bot: "random"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, data)
	}
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	finished := false
	for step := 0; step < 5000 && !finished; step++ {
		_, data := c.do("GET", "/games/"+id, tok, nil)
		v := decode[game.PlayerView](t, data)
		if v.Winner != nil {
			finished = true
			break
		}
		switch v.Phase {
		case game.PhaseDiscard:
			act := actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}}
			c.do("POST", "/games/"+id+"/actions", tok, act)
		case game.PhasePlay:
			if v.ToPlay != nil && *v.ToPlay == v.You && len(v.LegalPlays) > 0 {
				card := v.LegalPlays[0]
				c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "play", Card: &card})
			}
		}
	}
	if !finished {
		t.Fatal("game did not finish")
	}

	stored, _ := rs.ResultsForPlayer(uid, 50)
	if len(stored) != 1 {
		t.Fatalf("stored results = %d, want 1", len(stored))
	}
	// The opponent is seat1; it must be recorded as the random bot we selected.
	r := stored[0]
	rndVer := newBot("random").Version()
	if r.Bots[1].Name != "random" || r.Bots[1].Version != rndVer {
		t.Errorf("bot seat = %+v, want name=%q version=%q", r.Bots[1], "random", rndVer)
	}
	if r.Bots[1].Name == bot.DefaultName {
		t.Error("selected bot was recorded as the champion default, not the chosen bot")
	}
}

// TestBotIdentitySurvivesRestore checks that a persisted session re-seats the SAME
// production bot on restore (not always the champion), and that a legacy record
// with no recorded name falls back to the default so old games still play.
func TestBotIdentitySurvivesRestore(t *testing.T) {
	mkSession := func(b bot.Bot) *session {
		s := &session{
			id:   "g",
			game: game.New(game.Options{Deck: cryptoDeck{}, TargetScore: 121}),
		}
		s.bots[game.Seat1] = b
		return s
	}

	// Round-trip a session whose opponent is the random bot.
	rec := mkSession(newBot("random")).record()
	if rec.BotNames[game.Seat1] != "random" {
		t.Fatalf("record BotNames[1] = %q, want %q", rec.BotNames[game.Seat1], "random")
	}
	restored := sessionFromRecord(rec, nil)
	if restored.bots[game.Seat1] == nil || restored.bots[game.Seat1].Name() != "random" {
		t.Errorf("restored bot = %v, want the random bot", restored.bots[game.Seat1])
	}
	if restored.bots[game.Seat0] != nil {
		t.Errorf("human seat restored a bot: %v", restored.bots[game.Seat0])
	}

	// A legacy record (Bots set, no BotNames) restores the CHAMPION — those
	// games predate bot selection, so the champion is the opponent they were
	// actually played against, whatever the default has since become.
	legacy := Record{
		ID:   "old",
		Game: game.New(game.Options{Deck: cryptoDeck{}, TargetScore: 121}).Snapshot(),
		Bots: [2]bool{false, true},
	}
	old := sessionFromRecord(legacy, nil)
	if old.bots[game.Seat1] == nil || old.bots[game.Seat1].Name() != bot.ChampionName {
		t.Errorf("legacy restore = %v, want the champion", old.bots[game.Seat1])
	}
}
