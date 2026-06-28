package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// testClient drives the HTTP API.
type testClient struct {
	t  *testing.T
	ts *httptest.Server
}

func newTestClient(t *testing.T) *testClient {
	t.Helper()
	ts := httptest.NewServer(New().Handler())
	t.Cleanup(ts.Close)
	return &testClient{t: t, ts: ts}
}

func (c *testClient) do(method, path, token string, body any) (*http.Response, []byte) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, data
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, data)
	}
	return v
}

// TestFullGameVsBot plays a whole game over HTTP against a bot, picking legal
// moves from the snapshot, and checks it reaches a valid game over.
func TestFullGameVsBot(t *testing.T) {
	c := newTestClient(t)

	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, data)
	}
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	for step := 0; step < 5000; step++ {
		_, data := c.do("GET", "/games/"+id, tok, nil)
		v := decode[game.PlayerView](t, data)

		if v.Winner != nil {
			if v.Scores[*v.Winner] < 121 {
				t.Fatalf("winner has %d", v.Scores[*v.Winner])
			}
			return
		}
		switch v.Phase {
		case game.PhaseDiscard:
			act := actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}}
			if resp, data := c.do("POST", "/games/"+id+"/actions", tok, act); resp.StatusCode != 200 {
				t.Fatalf("discard: %d %s", resp.StatusCode, data)
			}
		case game.PhasePlay:
			if v.ToPlay == nil || *v.ToPlay != v.You {
				t.Fatalf("play phase but not my turn: %+v", v)
			}
			card := v.LegalPlays[0]
			act := actionRequest{Type: "play", Card: &card}
			if resp, data := c.do("POST", "/games/"+id+"/actions", tok, act); resp.StatusCode != 200 {
				t.Fatalf("play: %d %s", resp.StatusCode, data)
			}
		}
	}
	t.Fatal("game did not finish")
}

// TestVisibility checks that a seat's snapshot and deltas never expose a hidden
// card. We play a bot game and, at every snapshot, assert the opponent's hand,
// the crib (pre-show), and the deck are absent from the JSON.
func TestVisibility(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	full := cribbage.Deck()

	for step := 0; step < 5000; step++ {
		resp, body := c.do("GET", "/games/"+id, tok, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("snapshot: %d %s", resp.StatusCode, body)
		}
		v := decode[game.PlayerView](t, body)

		// Your hand, the pile, both players' played cards (face up), and the
		// starter are all public; collect them.
		allowed := map[cribbage.Card]bool{}
		for _, set := range [][]cribbage.Card{v.YourHand, v.Pile, v.YourPlayed, v.OpponentPlayed} {
			for _, cd := range set {
				allowed[cd] = true
			}
		}
		if v.Starter != nil {
			allowed[*v.Starter] = true
		}
		// Any card string in the snapshot body must be an allowed card. (Hidden
		// cards would appear as their "5H" string if leaked.)
		text := string(body)
		for _, cd := range full {
			if !allowed[cd] && strings.Contains(text, `"`+cd.String()+`"`) {
				// Could be a coincidental substring only if it's a real card token;
				// we quoted it, so this is a genuine leak unless it's the snapshot
				// during the show (hands revealed). Allow once the game is over.
				if v.Phase != game.PhaseComplete {
					t.Fatalf("step %d: snapshot leaked %s\n%s", step, cd, text)
				}
			}
		}

		if v.Winner != nil {
			return
		}
		switch v.Phase {
		case game.PhaseDiscard:
			c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}})
		case game.PhasePlay:
			card := v.LegalPlays[0]
			c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "play", Card: &card})
		}
	}
	t.Fatal("did not finish")
}

// TestDeltaNumericFieldsAlwaysPresent guards the wire contract: the numeric
// score fields must serialize even when zero, so the client never has to treat
// an absent field as zero (the source of an earlier NaN bug). Presence-optional
// fields should still be omitted when empty.
func TestDeltaNumericFieldsAlwaysPresent(t *testing.T) {
	b, err := json.Marshal(Delta{Seq: 1, Type: "card_played"})
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, key := range []string{`"points":0`, `"count":0`, `"total":0`, `"opponentCards":0`} {
		if !strings.Contains(js, key) {
			t.Errorf("Delta JSON missing %s: %s", key, js)
		}
	}
	for _, key := range []string{"seat", "card", "cards", "hand", "cut", "scores", "combos"} {
		if strings.Contains(js, `"`+key+`"`) {
			t.Errorf("Delta JSON should omit empty %q: %s", key, js)
		}
	}
}

// TestResumeFromClampsNegative ensures a hostile ?since=-N can't drive a negative
// index into events[last:] in the stream loop.
func TestResumeFromClampsNegative(t *testing.T) {
	if got := resumeFrom(httptest.NewRequest("GET", "/x?since=-5", nil)); got != 0 {
		t.Errorf("resumeFrom(since=-5) = %d, want 0", got)
	}
	if got := resumeFrom(httptest.NewRequest("GET", "/x?since=3", nil)); got != 3 {
		t.Errorf("resumeFrom(since=3) = %d, want 3", got)
	}
	if got := resumeFrom(httptest.NewRequest("GET", "/x", nil)); got != 0 {
		t.Errorf("resumeFrom(no since) = %d, want 0", got)
	}
}

// TestBodySizeLimited rejects an oversized request body instead of reading it all.
func TestBodySizeLimited(t *testing.T) {
	c := newTestClient(t)
	huge := json.RawMessage(`{"mode":"bot","bot":"` + strings.Repeat("a", 1<<20) + `"}`)
	if resp, _ := c.do("POST", "/games", "", huge); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized body: got %d, want 400", resp.StatusCode)
	}
}

func TestAuth(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	id := created.GameID

	// no token
	if resp, _ := c.do("GET", "/games/"+id, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", resp.StatusCode)
	}
	// wrong token
	if resp, _ := c.do("GET", "/games/"+id, "not-a-real-token", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: got %d, want 401", resp.StatusCode)
	}
	// unknown game
	if resp, _ := c.do("GET", "/games/nope", created.PlayerToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game: got %d, want 404", resp.StatusCode)
	}
	// valid
	if resp, _ := c.do("GET", "/games/"+id, created.PlayerToken, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("valid: got %d, want 200", resp.StatusCode)
	}
}

func TestOpenGameJoin(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "open"})
	created := decode[createResponse](t, data)

	// join on an unknown game id → 404
	if resp, _ := c.do("POST", "/games/nope/join", "", joinRequest{}); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game join: got %d, want 404", resp.StatusCode)
	}

	// the game id alone claims seat 1 — no token needed
	resp, jd := c.do("POST", "/games/"+created.GameID+"/join", "", joinRequest{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join: %d %s", resp.StatusCode, jd)
	}
	joined := decode[joinResponse](t, jd)
	if joined.Seat != game.Seat1 || joined.PlayerToken == "" {
		t.Fatalf("unexpected join response %+v", joined)
	}
	// the seat is now taken: a second join fails
	if resp, _ := c.do("POST", "/games/"+created.GameID+"/join", "", joinRequest{}); resp.StatusCode != http.StatusConflict {
		t.Errorf("second join: got %d, want 409", resp.StatusCode)
	}

	// both players can read their own (different) views
	for _, tok := range []string{created.PlayerToken, joined.PlayerToken} {
		if resp, _ := c.do("GET", "/games/"+created.GameID, tok, nil); resp.StatusCode != 200 {
			t.Errorf("view for %s: %d", tok, resp.StatusCode)
		}
	}
	// the joined seat-1 token can act and stream.
	if resp, _ := c.do("GET", "/games/"+created.GameID, joined.PlayerToken, nil); resp.StatusCode != 200 {
		t.Errorf("joined token snapshot: got %d, want 200", resp.StatusCode)
	}
	ch, sr, scancel := c.openStream(c.ts.URL+"/games/"+created.GameID+"/stream?token="+joined.PlayerToken, "")
	if sr.StatusCode != http.StatusOK {
		t.Errorf("joined token stream: got %d, want 200", sr.StatusCode)
	}
	nextOfType(t, ch, "players")
	sr.Body.Close()
	scancel()
}

func TestIllegalAndVersionConflict(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	// playing during the discard phase is illegal
	card := cribbage.Card{Rank: cribbage.Ace, Suit: cribbage.Spades}
	if resp, _ := c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "play", Card: &card}); resp.StatusCode != http.StatusConflict {
		t.Errorf("play in discard: got %d, want 409", resp.StatusCode)
	}

	// stale expected_version → 409
	_, sd := c.do("GET", "/games/"+id, tok, nil)
	v := decode[game.PlayerView](t, sd)
	stale := v.Version - 1
	act := actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}, ExpectedVersion: &stale}
	if resp, _ := c.do("POST", "/games/"+id+"/actions", tok, act); resp.StatusCode != http.StatusConflict {
		t.Errorf("stale version: got %d, want 409", resp.StatusCode)
	}
}

// TestConcurrentReads hammers a game with concurrent snapshot reads and an
// action, to surface data races under -race.
func TestConcurrentReads(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				c.do("GET", "/games/"+id, tok, nil)
			}
		}()
	}
	_, sd := c.do("GET", "/games/"+id, tok, nil)
	v := decode[game.PlayerView](t, sd)
	c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}})
	wg.Wait()
}

// TestStream connects to the SSE delta stream, reads the catch-up deltas sent on
// connect, then makes an action and confirms the new deltas are pushed.
func TestStream(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", c.ts.URL+"/games/"+id+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	lines := make(chan string, 64)
	go func() {
		rd := bufio.NewReader(resp.Body)
		for {
			line, err := rd.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if err != nil {
				return
			}
		}
	}()

	// count "data:" lines until we've seen the catch-up (>=1), then trigger more.
	waitForData := func(min int) {
		t.Helper()
		seen := 0
		for {
			select {
			case line := <-lines:
				if strings.HasPrefix(line, "data:") {
					if seen++; seen >= min {
						return
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("timed out waiting for %d data lines (saw %d)", min, seen)
			}
		}
	}

	waitForData(1) // catch-up on connect (cut/deal/bot-discard)

	// An action produces more deltas, which must be pushed to the stream.
	_, sd := c.do("GET", "/games/"+id, tok, nil)
	v := decode[game.PlayerView](t, sd)
	c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}})
	waitForData(1)
}

// TestAnalysis checks the analysis endpoint returns a ranked discard evaluation
// at the discard decision and a ranked play evaluation when it is the seat's turn
// to play, and that it leaks no hidden card (it is computed from View(seat)).
func TestAnalysis(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	tok, id := created.PlayerToken, created.GameID

	full := cribbage.Deck()
	sawDiscard, sawPlay := false, false

	for step := 0; step < 5000; step++ {
		_, sd := c.do("GET", "/games/"+id, tok, nil)
		v := decode[game.PlayerView](t, sd)
		if v.Winner != nil {
			break
		}

		resp, ad := c.do("GET", "/games/"+id+"/analysis", tok, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("analysis: %d %s", resp.StatusCode, ad)
		}
		a := decode[AnalysisResponse](t, ad)

		// No hidden card may appear anywhere in the analysis payload.
		allowed := map[cribbage.Card]bool{}
		for _, set := range [][]cribbage.Card{v.YourHand, v.Pile, v.YourPlayed, v.OpponentPlayed} {
			for _, cd := range set {
				allowed[cd] = true
			}
		}
		if v.Starter != nil {
			allowed[*v.Starter] = true
		}
		for _, cd := range full {
			if allowed[cd] {
				continue
			}
			if strings.Contains(string(ad), `"`+cd.String()+`"`) {
				t.Fatalf("analysis leaked hidden card %s: %s", cd, ad)
			}
		}

		switch v.Phase {
		case game.PhaseDiscard:
			if a.Phase == "discard" {
				if len(a.Discards) != 15 {
					t.Fatalf("want 15 ranked discards, got %d", len(a.Discards))
				}
				// Ranked best-first: scores must be non-increasing.
				for i := 1; i < len(a.Discards); i++ {
					if a.Discards[i].Score > a.Discards[i-1].Score+1e-9 {
						t.Fatalf("discards not sorted at %d", i)
					}
				}
				sawDiscard = true
			}
			c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "discard", Cards: []cribbage.Card{v.YourHand[0], v.YourHand[1]}})
		case game.PhasePlay:
			if v.ToPlay == nil || *v.ToPlay != v.You {
				t.Fatalf("play phase but not my turn")
			}
			if a.Phase == "play" {
				if len(a.Plays) != len(v.LegalPlays) {
					t.Fatalf("want %d ranked plays, got %d", len(v.LegalPlays), len(a.Plays))
				}
				sawPlay = true
			}
			card := v.LegalPlays[0]
			c.do("POST", "/games/"+id+"/actions", tok, actionRequest{Type: "play", Card: &card})
		}
	}

	if !sawDiscard {
		t.Error("never saw a discard analysis")
	}
	if !sawPlay {
		t.Error("never saw a play analysis")
	}
}

// sseEvent is one parsed SSE event: the optional id: line and the data: payload.
type sseEvent struct {
	id    string // "" if the event carried no id: line
	hasID bool
	data  []byte
}

// readSSE reads the stream body, parsing complete events (separated by a blank
// line) onto a channel. Comment lines (": ping") are skipped.
func readSSE(body io.Reader) <-chan sseEvent {
	out := make(chan sseEvent, 64)
	go func() {
		rd := bufio.NewReader(body)
		var cur sseEvent
		var haveData bool
		for {
			line, err := rd.ReadString('\n')
			trimmed := strings.TrimRight(line, "\r\n")
			switch {
			case trimmed == "":
				if haveData {
					out <- cur
				}
				cur, haveData = sseEvent{}, false
			case strings.HasPrefix(trimmed, ":"):
				// comment / heartbeat
			case strings.HasPrefix(trimmed, "id:"):
				cur.id = strings.TrimSpace(strings.TrimPrefix(trimmed, "id:"))
				cur.hasID = true
			case strings.HasPrefix(trimmed, "data:"):
				cur.data = []byte(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
				haveData = true
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// openStream connects to the SSE stream with the given URL (caller supplies auth
// via header or query). It returns the parsed-event channel and a cancel func.
func (c *testClient) openStream(url, bearerTok string) (<-chan sseEvent, *http.Response, func()) {
	c.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if bearerTok != "" {
		req.Header.Set("Authorization", "Bearer "+bearerTok)
	}
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		cancel()
		c.t.Fatal(err)
	}
	return readSSE(resp.Body), resp, cancel
}

// nextOfType reads events until one of the given type arrives (or times out),
// returning the decoded Delta and the raw event (for id inspection).
func nextOfType(t *testing.T, ch <-chan sseEvent, typ string) (Delta, sseEvent) {
	t.Helper()
	for {
		select {
		case ev := <-ch:
			d := decode[Delta](t, ev.data)
			if d.Type == typ {
				return d, ev
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q event", typ)
		}
	}
}

// TestTwoHumansPlayAndRedact opens a game, joins it, and has both seats discard;
// it confirms each seat's stream receives the other's moves and that per-seat
// redaction holds (no opponent hand card leaks into a seat's deltas).
func TestTwoHumansPlayAndRedact(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "open", Name: "Alice"})
	created := decode[createResponse](t, data)
	id, hostTok := created.GameID, created.PlayerToken

	_, jd := c.do("POST", "/games/"+id+"/join", "", joinRequest{Name: "Bob"})
	joined := decode[joinResponse](t, jd)
	guestTok := joined.PlayerToken

	hostCh, hr, hc := c.openStream(c.ts.URL+"/games/"+id+"/stream", hostTok)
	defer hr.Body.Close()
	defer hc()
	guestCh, gr, gc := c.openStream(c.ts.URL+"/games/"+id+"/stream", guestTok)
	defer gr.Body.Close()
	defer gc()

	// drain initial roster events from both
	nextOfType(t, hostCh, "players")
	nextOfType(t, guestCh, "players")

	// Host discards; the guest's stream must learn a "discarded" event (redacted).
	_, hv := c.do("GET", "/games/"+id, hostTok, nil)
	hview := decode[game.PlayerView](t, hv)
	hostHand := hview.YourHand
	c.do("POST", "/games/"+id+"/actions", hostTok, actionRequest{Type: "discard", Cards: []cribbage.Card{hostHand[0], hostHand[1]}})

	d, _ := nextOfType(t, guestCh, "discarded")
	// The guest must NOT see the host's actual discarded cards.
	if len(d.Cards) != 0 {
		t.Fatalf("guest saw host's discard cards: %+v", d.Cards)
	}
	// And those cards must not appear anywhere in the guest's raw event.
	for _, raw := range collectStreamRaw(guestCh, 200*time.Millisecond) {
		for _, cd := range hostHand[:2] {
			if strings.Contains(raw, `"`+cd.String()+`"`) {
				t.Fatalf("guest stream leaked host card %s: %s", cd, raw)
			}
		}
	}

	// Guest discards; the host's stream learns it.
	_, gv := c.do("GET", "/games/"+id, guestTok, nil)
	gview := decode[game.PlayerView](t, gv)
	c.do("POST", "/games/"+id+"/actions", guestTok, actionRequest{Type: "discard", Cards: []cribbage.Card{gview.YourHand[0], gview.YourHand[1]}})
	nextOfType(t, hostCh, "discarded")
}

// collectStreamRaw drains whatever events are immediately available within d.
func collectStreamRaw(ch <-chan sseEvent, d time.Duration) []string {
	var out []string
	deadline := time.After(d)
	for {
		select {
		case ev := <-ch:
			out = append(out, string(ev.data))
		case <-deadline:
			return out
		}
	}
}

// TestStreamResumeClampsAheadCursor: a stream resumed from a ?since= far beyond the
// event log must not be silently stranded — the cursor is clamped to the current
// end, so subsequent deltas still arrive.
func TestStreamResumeClampsAheadCursor(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "open", Name: "Alice"})
	created := decode[createResponse](t, data)
	id, hostTok := created.GameID, created.PlayerToken
	_, jd := c.do("POST", "/games/"+id+"/join", "", joinRequest{Name: "Bob"})
	guestTok := decode[joinResponse](t, jd).PlayerToken

	// Resume the host's stream from a cursor far beyond the log.
	hostCh, hr, hc := c.openStream(c.ts.URL+"/games/"+id+"/stream?since=9999&token="+hostTok, "")
	defer hr.Body.Close()
	defer hc()
	nextOfType(t, hostCh, "players") // the initial roster still arrives

	// A subsequent game event must reach the over-resumed stream — without the
	// clamp this would time out, since 9999 is never < len(events).
	_, gv := c.do("GET", "/games/"+id, guestTok, nil)
	gview := decode[game.PlayerView](t, gv)
	c.do("POST", "/games/"+id+"/actions", guestTok, actionRequest{Type: "discard", Cards: []cribbage.Card{gview.YourHand[0], gview.YourHand[1]}})
	nextOfType(t, hostCh, "discarded")
}

// TestStreamTokenQueryAuth confirms the stream accepts ?token= (for EventSource)
// and rejects a bad or absent token.
func TestStreamTokenQueryAuth(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	id, tok := created.GameID, created.PlayerToken

	// good token via query param
	ch, resp, cancel := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+tok, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("query-token stream: got %d, want 200", resp.StatusCode)
	}
	nextOfType(t, ch, "players")
	resp.Body.Close()
	cancel()

	// absent token rejected
	_, r2, c2 := c.openStream(c.ts.URL+"/games/"+id+"/stream", "")
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("no-token stream: got %d, want 401", r2.StatusCode)
	}
	r2.Body.Close()
	c2()

	// bad token rejected
	_, r3, c3 := c.openStream(c.ts.URL+"/games/"+id+"/stream?token=nope", "")
	if r3.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad-token stream: got %d, want 401", r3.StatusCode)
	}
	r3.Body.Close()
	c3()
}

// TestPlayersDelta verifies the players roster delta: it arrives on connect, it
// carries names + connected flags, it carries no id: line (so it can't disturb
// resume), and it is re-sent when the opponent joins and when a seat disconnects.
func TestPlayersDelta(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "open", Name: "Alice"})
	created := decode[createResponse](t, data)
	id, hostTok := created.GameID, created.PlayerToken

	hostCh, hr, hc := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+hostTok, "")
	defer hr.Body.Close()
	defer hc()

	// (a) players delta on connect — no id: line, host present, seat 1 empty.
	d, ev := nextOfType(t, hostCh, "players")
	if ev.hasID {
		t.Errorf("players delta carried an id: line: %+v", ev)
	}
	if d.Seq != 0 {
		t.Errorf("players delta Seq = %d, want 0", d.Seq)
	}
	if len(d.Players) != 2 {
		t.Fatalf("want 2 players entries, got %d", len(d.Players))
	}
	if d.Players[0].Name != "Alice" || !d.Players[0].Connected {
		t.Errorf("seat0 = %+v, want Alice connected", d.Players[0])
	}
	if d.Players[1].Name != "" || d.Players[1].Connected {
		t.Errorf("seat1 = %+v, want empty disconnected", d.Players[1])
	}

	// (b) opponent joins → host stream gets an updated roster with Bob's name.
	_, jd := c.do("POST", "/games/"+id+"/join", "", joinRequest{Name: "Bob"})
	joined := decode[joinResponse](t, jd)
	guestTok := joined.PlayerToken

	d2, _ := nextOfType(t, hostCh, "players")
	if d2.Players[1].Name != "Bob" {
		t.Errorf("after join seat1 name = %q, want Bob", d2.Players[1].Name)
	}
	// Bob hasn't opened a stream yet, so not connected.
	if d2.Players[1].Connected {
		t.Errorf("seat1 connected before any stream")
	}

	// Guest opens a stream → host learns seat1 is now connected.
	guestCh, gr, gcancel := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+guestTok, "")
	defer gr.Body.Close()
	nextOfType(t, guestCh, "players")
	d3, _ := nextOfType(t, hostCh, "players")
	if !d3.Players[1].Connected {
		t.Errorf("seat1 not connected after guest stream opened")
	}

	// (c) guest disconnects → host learns seat1 is disconnected again.
	gcancel()
	gr.Body.Close()
	d4, _ := nextOfType(t, hostCh, "players")
	if d4.Players[1].Connected {
		t.Errorf("seat1 still connected after guest disconnect")
	}
}

// TestAbandon opens a game, joins it, both seats subscribe, then seat 0 abandons.
// Seat 0 gets 204, and seat 1's stream receives a "players" roster delta where
// seat 0's entry has left:true.
func TestAbandon(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "open", Name: "Alice"})
	created := decode[createResponse](t, data)
	id, hostTok := created.GameID, created.PlayerToken

	_, jd := c.do("POST", "/games/"+id+"/join", "", joinRequest{Name: "Bob"})
	guestTok := decode[joinResponse](t, jd).PlayerToken

	hostCh, hr, hc := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+hostTok, "")
	defer hr.Body.Close()
	defer hc()
	guestCh, gr, gc := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+guestTok, "")
	defer gr.Body.Close()
	defer gc()

	// drain initial rosters from both streams
	nextOfType(t, hostCh, "players")
	nextOfType(t, guestCh, "players")

	// seat 0 abandons → 204 with an empty body.
	resp, body := c.do("POST", "/games/"+id+"/abandon", hostTok, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("abandon: got %d, want 204", resp.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("abandon body should be empty, got %q", body)
	}

	// idempotent: abandoning again is fine.
	if resp, _ := c.do("POST", "/games/"+id+"/abandon", hostTok, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("second abandon: got %d, want 204", resp.StatusCode)
	}

	// seat 1's stream learns seat 0 has left for good.
	for {
		d, _ := nextOfType(t, guestCh, "players")
		if d.Players[0].Left {
			if d.Players[1].Left {
				t.Errorf("seat1 marked left without abandoning: %+v", d.Players[1])
			}
			break
		}
	}
}

// TestAbandonAuth confirms abandon requires a valid token and a known game.
func TestAbandonAuth(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	id := created.GameID

	if resp, _ := c.do("POST", "/games/"+id+"/abandon", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", resp.StatusCode)
	}
	if resp, _ := c.do("POST", "/games/"+id+"/abandon", "not-a-real-token", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: got %d, want 401", resp.StatusCode)
	}
	if resp, _ := c.do("POST", "/games/nope/abandon", created.PlayerToken, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown game: got %d, want 404", resp.StatusCode)
	}
}

// TestReaperSkipsConnected confirms a session with a live stream subscriber is
// never reaped even when its lastSeen is stale, while one without is.
func TestReaperSkipsConnected(t *testing.T) {
	srv := New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := ts.Client()

	mk := func() string {
		b, _ := json.Marshal(createRequest{Mode: "open"})
		resp, _ := client.Post(ts.URL+"/games", "application/json", bytes.NewReader(b))
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return decode[createResponse](t, data).GameID
	}

	connectedID := mk()
	idleID := mk()

	// Open a stream on connectedID and wait for the roster so the subscriber is
	// registered server-side.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connected := srv.reg
	connectedSess, _ := connected.get(connectedID)
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/games/"+connectedID+"/stream?token="+connectedSess.tokens[game.Seat0], nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ch := readSSE(resp.Body)
	nextOfType(t, ch, "players")

	// Force both sessions' lastSeen far into the past, then reap.
	for _, gid := range []string{connectedID, idleID} {
		s, _ := srv.reg.get(gid)
		s.mu.Lock()
		s.lastSeen = time.Now().Add(-time.Hour)
		s.mu.Unlock()
	}
	n := srv.Reap(time.Minute)
	if n != 1 {
		t.Fatalf("reaped %d sessions, want 1 (only the idle one)", n)
	}
	if _, ok := srv.reg.get(connectedID); !ok {
		t.Error("connected session was reaped")
	}
	if _, ok := srv.reg.get(idleID); ok {
		t.Error("idle session survived reaping")
	}
}

// TestCleanNameRuneSafe ensures truncation caps at 24 runes (not bytes) and never
// splits a multi-byte UTF-8 sequence, which would ship mojibake.
func TestCleanNameRuneSafe(t *testing.T) {
	// 30 multi-byte runes (accented Latin); 30 * 2 bytes = 60 bytes.
	name := strings.Repeat("é", 30)
	got := cleanName(name)
	if !utf8.ValidString(got) {
		t.Errorf("cleanName produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 24 {
		t.Errorf("cleanName kept %d runes, want <= 24", n)
	}
	if n := utf8.RuneCountInString(got); n != 24 {
		t.Errorf("cleanName(30 runes) kept %d runes, want exactly 24", n)
	}

	// Emoji (4-byte runes) must likewise survive intact.
	emoji := cleanName(strings.Repeat("😀", 30))
	if !utf8.ValidString(emoji) {
		t.Errorf("cleanName produced invalid UTF-8 from emoji: %q", emoji)
	}
	if n := utf8.RuneCountInString(emoji); n != 24 {
		t.Errorf("cleanName(30 emoji) kept %d runes, want 24", n)
	}

	// A short ASCII name is unchanged.
	if got := cleanName(" Alice "); got != "Alice" {
		t.Errorf("cleanName(\" Alice \") = %q, want \"Alice\"", got)
	}
}

// TestRegistryCapBoundary checks registry.add enforces the maxSessions ceiling:
// adds succeed up to the cap, then the next add returns false without inserting.
func TestRegistryCapBoundary(t *testing.T) {
	reg := newRegistry()
	// Fill to capacity using lightweight sessions (no real games allocated).
	for i := 0; i < maxSessions; i++ {
		s := &session{id: strconv.Itoa(i)}
		if !reg.add(s) {
			t.Fatalf("add %d returned false before reaching cap", i)
		}
	}
	if len(reg.sessions) != maxSessions {
		t.Fatalf("registry has %d sessions, want %d", len(reg.sessions), maxSessions)
	}
	// The next add must be rejected and must not insert.
	if reg.add(&session{id: "overflow"}) {
		t.Error("add over capacity returned true, want false")
	}
	if _, ok := reg.sessions["overflow"]; ok {
		t.Error("rejected session was inserted into the registry")
	}
	if len(reg.sessions) != maxSessions {
		t.Errorf("registry grew past cap to %d", len(reg.sessions))
	}
}

// TestCreateAtCapacity503 confirms POST /games returns 503 when the registry is
// full. We pre-fill the registry directly to avoid allocating real games.
func TestCreateAtCapacity503(t *testing.T) {
	srv := New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for i := 0; i < maxSessions; i++ {
		srv.reg.add(&session{id: strconv.Itoa(i)})
	}

	b, _ := json.Marshal(createRequest{Mode: "bot"})
	resp, err := ts.Client().Post(ts.URL+"/games", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("create at capacity: got %d, want 503", resp.StatusCode)
	}
}

// TestGracefulShutdownEndsStream opens a real SSE stream, calls srv.Close(), and
// asserts the stream handler returns promptly (the response body ends, no hang).
func TestGracefulShutdownEndsStream(t *testing.T) {
	srv := New()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	b, _ := json.Marshal(createRequest{Mode: "bot"})
	resp, _ := ts.Client().Post(ts.URL+"/games", "application/json", bytes.NewReader(b))
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	created := decode[createResponse](t, data)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/games/"+created.GameID+"/stream?token="+created.PlayerToken, nil)
	sr, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Body.Close()

	// Wait for the roster so the handler is fully subscribed and in its loop.
	ch := readSSE(sr.Body)
	nextOfType(t, ch, "players")

	// Signal shutdown; the handler must return, ending the response body.
	srv.Close()

	done := make(chan struct{})
	go func() {
		io.Copy(io.Discard, sr.Body) // returns when the handler returns
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream handler did not return after Close()")
	}
}

// TestRegistryCount confirms registry.count tracks the live session map length.
func TestRegistryCount(t *testing.T) {
	reg := newRegistry()
	if got := reg.count(); got != 0 {
		t.Fatalf("empty registry count = %d, want 0", got)
	}
	for i := 0; i < 5; i++ {
		reg.add(&session{id: strconv.Itoa(i)})
	}
	if got := reg.count(); got != 5 {
		t.Fatalf("count after 5 adds = %d, want 5", got)
	}
}

// TestStatsEndpoint checks GET /stats reports the live game count after creating
// N games, and that subscribers reflects an open SSE stream (rising to >=1 while
// open, dropping back when it closes).
func TestStatsEndpoint(t *testing.T) {
	c := newTestClient(t)

	// No games yet.
	resp, data := c.do("GET", "/stats", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: %d %s", resp.StatusCode, data)
	}
	st := decode[statsResponse](t, data)
	if st.Games != 0 || st.Subscribers != 0 {
		t.Fatalf("initial stats = %+v, want games=0 subscribers=0", st)
	}

	// Create N games; /stats.games must reflect them.
	const n = 3
	var id, tok string
	for i := 0; i < n; i++ {
		_, cd := c.do("POST", "/games", "", createRequest{Mode: "bot"})
		created := decode[createResponse](t, cd)
		id, tok = created.GameID, created.PlayerToken
	}
	_, data = c.do("GET", "/stats", "", nil)
	if st = decode[statsResponse](t, data); st.Games != n {
		t.Fatalf("stats.games = %d, want %d", st.Games, n)
	}
	if st.Subscribers != 0 {
		t.Fatalf("stats.subscribers = %d before any stream, want 0", st.Subscribers)
	}

	// Open a stream: subscribers must rise to >= 1.
	ch, sr, cancel := c.openStream(c.ts.URL+"/games/"+id+"/stream?token="+tok, "")
	nextOfType(t, ch, "players") // ensure the subscriber is registered server-side
	_, data = c.do("GET", "/stats", "", nil)
	if st = decode[statsResponse](t, data); st.Subscribers < 1 {
		t.Fatalf("stats.subscribers = %d with an open stream, want >= 1", st.Subscribers)
	}

	// Close the stream: subscribers must drop back to 0.
	cancel()
	sr.Body.Close()
	deadline := time.After(2 * time.Second)
	for {
		_, data = c.do("GET", "/stats", "", nil)
		if decode[statsResponse](t, data).Subscribers == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("subscribers did not drop back to 0 after stream close")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// TestStatusRecorder unit-tests the logging middleware's status wrapper: it
// records an explicit status, defaults to 200 when WriteHeader is never called,
// and still exposes http.Flusher so SSE streaming survives the wrapper.
func TestStatusRecorder(t *testing.T) {
	// Default status is 200 when WriteHeader is never called.
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	sr.Write([]byte("ok"))
	if sr.status != http.StatusOK {
		t.Errorf("default status = %d, want 200", sr.status)
	}

	// An explicit WriteHeader is recorded and forwarded.
	rec = httptest.NewRecorder()
	sr = &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	sr.WriteHeader(http.StatusTeapot)
	if sr.status != http.StatusTeapot {
		t.Errorf("recorded status = %d, want %d", sr.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("underlying status = %d, want %d", rec.Code, http.StatusTeapot)
	}

	// The wrapper must implement http.Flusher (the SSE stream type-asserts it).
	var _ http.Flusher = sr
	if _, ok := http.ResponseWriter(sr).(http.Flusher); !ok {
		t.Error("statusRecorder does not satisfy http.Flusher; SSE streaming would break")
	}
}

// TestLogRequestsLine captures log output and asserts the middleware logs one
// line with method, path, status, and a duration in ms.
func TestLogRequestsLine(t *testing.T) {
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/games/abc", nil))

	line := buf.String()
	for _, want := range []string{"GET", "/games/abc", "418", "ms"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %q", line, want)
		}
	}
}

// TestAnalysisAuth confirms the analysis endpoint requires a valid token.
func TestAnalysisAuth(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	created := decode[createResponse](t, data)
	if resp, _ := c.do("GET", "/games/"+created.GameID+"/analysis", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", resp.StatusCode)
	}
}
