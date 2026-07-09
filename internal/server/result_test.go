package server

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestResultMetaGobRoundTrip guards the version metadata blob the Postgres store
// persists: it must survive gob encode/decode unchanged.
func TestResultMetaGobRoundTrip(t *testing.T) {
	m := resultMeta{
		EngineVersion: game.EngineVersion,
		Bots:          [2]BotInfo{{}, {Name: bot.DefaultName, Version: bot.Champion().Version()}},
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(m); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var got resultMeta
	if err := gob.NewDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != m {
		t.Fatalf("meta changed across gob round-trip: got %+v, want %+v", got, m)
	}
}

// flakyResultStore fails its first failsLeft SaveResult calls, then delegates.
type flakyResultStore struct {
	*MemResultStore
	failsLeft int
}

func (f *flakyResultStore) SaveResult(r Result) error {
	if f.failsLeft > 0 {
		f.failsLeft--
		return errors.New("transient db error")
	}
	return f.MemResultStore.SaveResult(r)
}

func TestSaveResultRetries(t *testing.T) {
	res := Result{ID: "g1", PlayerIDs: [2]string{"u1", ""}, Names: [2]string{"A", "B"}, Scores: [2]int{121, 90}, Winner: 0, EndedAt: time.Now()}

	// Recovers after a few transient failures (instant: a no-op sleep).
	mem := NewMemResultStore()
	fs := &flakyResultStore{MemResultStore: mem, failsLeft: 3}
	if err := saveResultRetrying(fs, res, func(time.Duration) {}); err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if total, _, _ := mem.PlayerStats("u1"); total != 1 {
		t.Errorf("result not saved after retries (total=%d)", total)
	}

	// Gives up (returns the error) once failures exceed the retry budget.
	fs2 := &flakyResultStore{MemResultStore: NewMemResultStore(), failsLeft: 99}
	if err := saveResultRetrying(fs2, res, func(time.Duration) {}); err == nil {
		t.Error("expected an error after exhausting retries")
	}
}

func TestReadyz(t *testing.T) {
	c := newTestClient(t) // default store isn't a pinger -> always ready
	if resp, body := c.do("GET", "/readyz", "", nil); resp.StatusCode != http.StatusOK {
		t.Errorf("readyz: got %d %s, want 200", resp.StatusCode, body)
	}
}

func TestMemResultStore(t *testing.T) {
	rs := NewMemResultStore()
	mk := func(id, p0, p1 string, winner int, ago time.Duration) Result {
		return Result{
			ID: id, PlayerIDs: [2]string{p0, p1}, Names: [2]string{"A", "B"},
			Scores: [2]int{121, 100}, Winner: winner, Events: []game.Event{},
			EndedAt: time.Now().Add(-ago),
		}
	}
	rs.SaveResult(mk("g1", "u1", "u2", 0, 3*time.Minute))  // u1 seat0 wins
	rs.SaveResult(mk("g2", "u3", "u1", 0, 1*time.Minute))  // u1 seat1, winner seat0 -> u1 loses
	rs.SaveResult(mk("g3", "u2", "u3", 1, 2*time.Minute))  // no u1
	rs.SaveResult(mk("g1", "u1", "u2", 0, 99*time.Minute)) // duplicate id -> ignored

	games, _ := rs.ResultsForPlayer("u1", 50)
	if len(games) != 2 {
		t.Fatalf("u1 games = %d, want 2", len(games))
	}
	if games[0].ID != "g2" || games[1].ID != "g1" {
		t.Errorf("order = %s,%s, want newest-first g2,g1", games[0].ID, games[1].ID)
	}
	if games[0].Events != nil {
		t.Error("the list should omit the events blob")
	}
	total, wins, _ := rs.PlayerStats("u1")
	if total != 2 || wins != 1 {
		t.Errorf("u1 stats = %d/%d, want 2 total / 1 win", total, wins)
	}
	if g, _ := rs.ResultsForPlayer("nobody", 50); len(g) != 0 {
		t.Errorf("a stranger has %d games, want 0", len(g))
	}
}

// authedClient is a cookie-jar client (for the session); game actions still pass
// the player token as a Bearer header.
type authedClient struct {
	t   *testing.T
	ts  *httptest.Server
	cli *http.Client
}

func newAuthedServer(t *testing.T) (*authedClient, *MemResultStore) {
	t.Helper()
	srv := New()
	rs := NewMemResultStore()
	srv.SetResultStore(rs)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &authedClient{t: t, ts: ts, cli: &http.Client{Jar: jar}}, rs
}

func (c *authedClient) do(method, path, token string, body any) (*http.Response, []byte) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, r)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.cli.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func (c *authedClient) signup(username string) string {
	c.t.Helper()
	_, data := c.do("POST", "/auth/signup", "", signupRequest{
		Username: username, Email: username + "@example.com", Password: "password123",
	})
	return decode[userResponse](c.t, data).ID
}

func TestUserGamesEndpoint(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")
	rs.SaveResult(Result{ID: "g1", PlayerIDs: [2]string{uid, "u2"}, Names: [2]string{"Alice", "Bob"}, Scores: [2]int{121, 90}, Winner: 0, EndedAt: time.Now().Add(-1 * time.Minute)})
	rs.SaveResult(Result{ID: "g2", PlayerIDs: [2]string{"u3", uid}, Names: [2]string{"Cara", "Alice"}, Scores: [2]int{121, 80}, Winner: 0, EndedAt: time.Now().Add(-2 * time.Minute)})
	rs.SaveResult(Result{ID: "g3", PlayerIDs: [2]string{"u2", "u3"}, Names: [2]string{"Bob", "Cara"}, Scores: [2]int{121, 50}, Winner: 0, EndedAt: time.Now()})

	resp, data := c.do("GET", "/users/me/games", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("games: %d %s", resp.StatusCode, data)
	}
	got := decode[userGamesResponse](t, data)
	if len(got.Games) != 2 {
		t.Fatalf("games = %d, want 2 (excludes the game alice isn't in)", len(got.Games))
	}
	if g := got.Games[0]; g.ID != "g1" || g.Opponent != "Bob" || g.YourScore != 121 || g.OpponentScore != 90 || !g.Won {
		t.Errorf("g1 (seat0 win) projected wrong: %+v", g)
	}
	if g := got.Games[1]; g.ID != "g2" || g.Opponent != "Cara" || g.YourScore != 80 || g.OpponentScore != 121 || g.Won {
		t.Errorf("g2 (seat1 loss) projected wrong: %+v", g)
	}
	if got.Stats != (playerStats{Total: 2, Wins: 1, Losses: 1}) {
		t.Errorf("stats = %+v, want 2/1/1", got.Stats)
	}

	// Unauthenticated -> 401.
	plain := &http.Client{}
	req, _ := http.NewRequest("GET", c.ts.URL+"/users/me/games", nil)
	r2, _ := plain.Do(req)
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("guest /users/me/games: %d, want 401", r2.StatusCode)
	}
	r2.Body.Close()
}

// TestGameOverRecordsResult plays a logged-in bot game to completion over HTTP and
// asserts the finished game lands in the player's history.
func TestGameOverRecordsResult(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("dave")

	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
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

	_, data = c.do("GET", "/users/me/games", "", nil)
	got := decode[userGamesResponse](t, data)
	if len(got.Games) != 1 {
		t.Fatalf("dave's history = %d games, want 1", len(got.Games))
	}
	if got.Games[0].ID != id {
		t.Errorf("history game id = %s, want %s", got.Games[0].ID, id)
	}
	if got.Stats.Total != 1 {
		t.Errorf("stats total = %d, want 1", got.Stats.Total)
	}

	// The stored record carries the engine version and the bot seat's name+version
	// (the creator is the human seat0; the opponent in "bot" mode is seat1).
	stored, _ := rs.ResultsForPlayer(uid, 50)
	if len(stored) != 1 {
		t.Fatalf("stored results = %d, want 1", len(stored))
	}
	r := stored[0]
	if r.EngineVersion != game.EngineVersion {
		t.Errorf("engine version = %q, want %q", r.EngineVersion, game.EngineVersion)
	}
	if r.Bots[0] != (BotInfo{}) {
		t.Errorf("human seat recorded a bot: %+v", r.Bots[0])
	}
	if r.Bots[1].Name != bot.DefaultName || r.Bots[1].Version != bot.Champion().Version() {
		t.Errorf("bot seat = %+v, want name=%q version=%q", r.Bots[1], bot.DefaultName, bot.Champion().Version())
	}
}

// TestPgResultStore is gated on TEST_DATABASE_URL. It exercises the results table:
// save (with a guest/NULL seat), idempotent re-save, per-player query + ordering,
// and stats.
func TestPgResultStore(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run the Postgres result-store integration test")
	}
	rs, err := NewPgResultStore(openTestDB(t, dsn))
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := rs.db.Exec("DELETE FROM results"); err != nil {
		t.Fatalf("clean results: %v", err)
	}

	now := time.Now()
	must := func(r Result) {
		if err := rs.SaveResult(r); err != nil {
			t.Fatalf("save %s: %v", r.ID, err)
		}
	}
	must(Result{ID: "a", PlayerIDs: [2]string{"p1", "p2"}, Names: [2]string{"P1", "P2"}, Scores: [2]int{121, 95}, Winner: 0, Events: []game.Event{}, EndedAt: now.Add(-2 * time.Minute), EngineVersion: game.EngineVersion, Bots: [2]BotInfo{{}, {Name: bot.DefaultName, Version: bot.Champion().Version()}}})
	must(Result{ID: "b", PlayerIDs: [2]string{"", "p1"}, Names: [2]string{"Guest", "P1"}, Scores: [2]int{121, 60}, Winner: 0, Events: []game.Event{}, EndedAt: now.Add(-1 * time.Minute)})
	must(Result{ID: "a", PlayerIDs: [2]string{"p1", "p2"}, Names: [2]string{"X", "Y"}, Scores: [2]int{0, 0}, Winner: 1, Events: []game.Event{}, EndedAt: now}) // dup id -> ignored

	games, err := rs.ResultsForPlayer("p1", 50)
	if err != nil {
		t.Fatalf("ResultsForPlayer: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("p1 games = %d, want 2", len(games))
	}
	if games[0].ID != "b" || games[1].ID != "a" {
		t.Errorf("order = %s,%s, want newest-first b,a", games[0].ID, games[1].ID)
	}
	if games[0].PlayerIDs[0] != "" {
		t.Errorf("guest seat round-tripped as %q, want empty", games[0].PlayerIDs[0])
	}
	// Game "a" (games[1]) carried version metadata; it must survive the round-trip.
	if a := games[1]; a.EngineVersion != game.EngineVersion ||
		a.Bots[0] != (BotInfo{}) || a.Bots[1] != (BotInfo{Name: bot.DefaultName, Version: bot.Champion().Version()}) {
		t.Errorf("version metadata round-trip wrong: engine=%q bots=%+v", a.EngineVersion, a.Bots)
	}
	total, wins, err := rs.PlayerStats("p1")
	if err != nil {
		t.Fatalf("PlayerStats: %v", err)
	}
	if total != 2 || wins != 1 {
		t.Errorf("p1 stats = %d/%d, want 2/1 (won 'a' as seat0, lost 'b' as seat1)", total, wins)
	}
	// An empty player id must not match the NULL guest seats.
	if g, _ := rs.ResultsForPlayer("", 50); len(g) != 0 {
		t.Errorf("empty playerID matched %d rows, want 0", len(g))
	}
}
