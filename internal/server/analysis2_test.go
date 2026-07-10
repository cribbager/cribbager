package server

import (
	"bytes"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// mlVsChampionEvents plays one complete seeded game — the ml bot on seat 0,
// the champion on seat 1 — and returns its event log. Both bots are
// deterministic, so the log is a fixed corpus for the analysis fidelity
// checks below.
func mlVsChampionEvents(t *testing.T, seed int64) []game.Event {
	t.Helper()
	ml, err := bot.New(bot.DefaultName, rand.New(rand.NewSource(seed)))
	if err != nil {
		t.Fatalf("build ml bot: %v", err)
	}
	_, events, err := bot.PlayGameEvents(ml, bot.Champion(), game.NewSeededDeck(seed))
	if err != nil {
		t.Fatalf("play game: %v", err)
	}
	return events
}

// engineIndex finds an engine by name in the response's engine list.
func engineIndex(t *testing.T, resp gameAnalysisV2Response, name string) int {
	t.Helper()
	for i, e := range resp.Engines {
		if e.Name == name {
			return i
		}
	}
	t.Fatalf("engine %q not in %v", name, resp.Engines)
	return -1
}

// assertEngineOptimal requires that the named engine grades EVERY decision of
// the analyzed seat as optimal with delta 0 — the strongest correctness
// check: the analyzed seat was played by that engine's own bot.
func assertEngineOptimal(t *testing.T, resp gameAnalysisV2Response, name string) {
	t.Helper()
	idx := engineIndex(t, resp, name)
	for _, d := range resp.Deals {
		dv := d.Discard.Engines[idx]
		if !dv.Optimal || dv.Delta != 0 {
			t.Errorf("deal %d: %s discard graded sub-optimal (delta %v) for its own bot's throw %v (best %v)",
				d.Deal, name, dv.Delta, d.Discard.Throw, dv.BestThrow)
		}
		if !d.Rollup[idx].DiscardOptimal {
			t.Errorf("deal %d: %s rollup discard_optimal = false", d.Deal, name)
		}
		for pi, p := range d.Plays {
			pv := p.Engines[idx]
			if !pv.Optimal || pv.Delta != 0 {
				t.Errorf("deal %d play %d: %s graded sub-optimal (delta %v) for its own bot's play %v (best %v, legal %v)",
					d.Deal, pi, name, pv.Delta, p.Played, pv.Best, p.Legal)
			}
		}
		if !d.Rollup[idx].PeggingOptimal {
			t.Errorf("deal %d: %s rollup pegging_optimal = false", d.Deal, name)
		}
	}
	s := resp.Summary[idx]
	if s.OptimalDiscards != s.Hands || s.OptimalPlays != s.PlayDecisions {
		t.Errorf("%s summary: %d/%d discards, %d/%d plays optimal — want all",
			name, s.OptimalDiscards, s.Hands, s.OptimalPlays, s.PlayDecisions)
	}
	if s.DiscardDeltaPoints != 0 || s.DiscardDeltaWinprob != 0 || s.PlayDeltaPoints != 0 || s.PlayDeltaWinprob != 0 {
		t.Errorf("%s summary deltas nonzero: %+v", name, s)
	}
}

// assertResponseInvariants checks the structural contract on any analysis
// response: engine order and versions, per-decision arrays aligned with
// engines[], non-forced plays only, delta/value arithmetic, agreement flags
// consistent with the engines' own bests, units well-formed (ml and exact-ev
// always points; champion's units match InReach at the decision's scores for
// discards), and start_scores matching an independent DealStats accumulation.
func assertResponseInvariants(t *testing.T, resp gameAnalysisV2Response, events []game.Event) {
	t.Helper()

	wantEngines := []engineInfo{
		{Name: "ml", Version: bot.NewMLAnalyzer().Version()},
		{Name: "champion", Version: bot.Champion().Version()},
		{Name: "exact-ev", Version: exactEVVersion},
	}
	if len(resp.Engines) != len(wantEngines) {
		t.Fatalf("engines = %v, want %v", resp.Engines, wantEngines)
	}
	for i, e := range wantEngines {
		if resp.Engines[i] != e {
			t.Errorf("engines[%d] = %v, want %v", i, resp.Engines[i], e)
		}
	}
	if len(resp.Summary) != len(resp.Engines) {
		t.Fatalf("summary has %d entries for %d engines", len(resp.Summary), len(resp.Engines))
	}

	// Independent start-score reconstruction via DealStats.
	stats := bot.DealStats(events)
	if len(resp.Deals) != len(stats) {
		t.Fatalf("deals = %d, want %d (every dealt hand is discarded in)", len(resp.Deals), len(stats))
	}
	var cum [2]int
	mlIdx := engineIndex(t, resp, "ml")
	champIdx := engineIndex(t, resp, "champion")
	evIdx := engineIndex(t, resp, "exact-ev")
	sawWinprob := false

	for k, d := range resp.Deals {
		if d.Deal != k {
			t.Errorf("deal index %d at position %d", d.Deal, k)
		}
		if d.StartScores != cum {
			t.Errorf("deal %d start_scores = %v, want %v (DealStats accumulation)", k, d.StartScores, cum)
		}
		// The engine caps the winner's board score at the target on the winning
		// award, but no HandDealt ever follows a win, so a full-value event sum
		// (this accumulation and analysis2's own) can never put a deal's
		// start_scores at or past the target. These games end mid-final-deal,
		// making that the interesting case.
		for s := 0; s < 2; s++ {
			if d.StartScores[s] >= 121 {
				t.Errorf("deal %d start_scores[%d] = %d, at or past the target", k, s, d.StartScores[s])
			}
		}
		if d.Dealer != int(stats[k].Dealer) {
			t.Errorf("deal %d dealer = %d, want %d", k, d.Dealer, stats[k].Dealer)
		}
		for s := game.Seat(0); s < 2; s++ {
			cum[s] += stats[k].Total(s)
		}

		// Discard block shape + engine verdicts.
		if len(d.Discard.Hand) != 6 || len(d.Discard.Engines) != len(resp.Engines) {
			t.Fatalf("deal %d discard block malformed: %+v", k, d.Discard)
		}
		agree := true
		for i, v := range d.Discard.Engines {
			if v.Delta < 0 || v.BestValue < v.ChosenValue-1e-6 {
				t.Errorf("deal %d engine %d: best %v < chosen %v (delta %v)", k, i, v.BestValue, v.ChosenValue, v.Delta)
			}
			if v.Units != unitsPoints && v.Units != unitsWinprob {
				t.Errorf("deal %d engine %d: bad units %q", k, i, v.Units)
			}
			if !samePair(v.BestThrow, d.Discard.Engines[0].BestThrow) {
				agree = false
			}
		}
		if d.Discard.Agree != agree {
			t.Errorf("deal %d discard agree = %v, want %v", k, d.Discard.Agree, agree)
		}
		if v := d.Discard.Engines[mlIdx]; v.Units != unitsPoints {
			t.Errorf("deal %d: ml units = %q, want points", k, v.Units)
		}
		if v := d.Discard.Engines[evIdx]; v.Units != unitsPoints {
			t.Errorf("deal %d: exact-ev units = %q, want points", k, v.Units)
		}
		// Champion's discard units flip exactly when the deal's start position
		// is in reach of the target for the analyzed seat.
		my, opp := d.StartScores[resp.Seat], d.StartScores[1-resp.Seat]
		wantUnits := unitsPoints
		if eval.InReach(my, opp, d.Dealer == resp.Seat) {
			wantUnits = unitsWinprob
		}
		if v := d.Discard.Engines[champIdx]; v.Units != wantUnits {
			t.Errorf("deal %d: champion discard units = %q, want %q at %v", k, v.Units, wantUnits, d.StartScores)
		}
		if wantUnits == unitsWinprob {
			sawWinprob = true
		}

		// Plays: non-forced only, aligned engine arrays, agreement by rank.
		for pi, p := range d.Plays {
			if distinctRanks(p.Legal) < 2 {
				t.Errorf("deal %d play %d: forced move (legal %v) should not be a decision", k, pi, p.Legal)
			}
			if len(p.Engines) != len(resp.Engines) {
				t.Fatalf("deal %d play %d: %d engine verdicts", k, pi, len(p.Engines))
			}
			pAgree := true
			for i, v := range p.Engines {
				if v.Delta < 0 {
					t.Errorf("deal %d play %d engine %d: negative delta %v", k, pi, i, v.Delta)
				}
				if v.Units != unitsPoints && v.Units != unitsWinprob {
					t.Errorf("deal %d play %d engine %d: bad units %q", k, pi, i, v.Units)
				}
				if v.Best.Rank != p.Engines[0].Best.Rank {
					pAgree = false
				}
			}
			if p.Agree != pAgree {
				t.Errorf("deal %d play %d agree = %v, want %v", k, pi, p.Agree, pAgree)
			}
			for _, idx := range []int{mlIdx, evIdx} {
				if v := p.Engines[idx]; v.Units != unitsPoints {
					t.Errorf("deal %d play %d: engine %d units = %q, want points", k, pi, idx, v.Units)
				}
			}
		}
		if len(d.Rollup) != len(resp.Engines) {
			t.Fatalf("deal %d rollup has %d entries", k, len(d.Rollup))
		}
	}
	// A full game to 121 always ends in reach, so the champion engine must
	// have switched to win-probability units at least once.
	if !sawWinprob {
		t.Error("no champion winprob-units discard verdict in a full game — the endgame switch never fired")
	}
}

// TestAnalysisV2EngineOptimality is the core fidelity check over the login
// (stored-Result) path: a finished ml-vs-champion game is analyzed from each
// seat, and each seat's own engine must grade every decision optimal. The
// response's structural invariants and determinism are checked too.
func TestAnalysisV2EngineOptimality(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")
	events := mlVsChampionEvents(t, 7)

	// The same game twice: alice on the ml seat, then on the champion seat.
	rs.SaveResult(Result{ID: "g-ml", PlayerIDs: [2]string{uid, "opp"}, Events: events, EndedAt: time.Now()})
	rs.SaveResult(Result{ID: "g-champ", PlayerIDs: [2]string{"opp", uid}, Events: events, EndedAt: time.Now()})

	resp, data := c.do("GET", "/games/g-ml/analysis", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("analysis: %d %s", resp.StatusCode, data)
	}
	got := decode[gameAnalysisV2Response](t, data)
	if got.GameID != "g-ml" || got.Seat != 0 {
		t.Fatalf("game/seat = %s/%d, want g-ml/0", got.GameID, got.Seat)
	}
	// Contract: no field is ever null — empty arrays encode as [] (e.g. the
	// pile on a lead play).
	if bytes.Contains(data, []byte("null")) {
		t.Error("response contains a null; the contract promises [] for empty arrays")
	}
	assertResponseInvariants(t, got, events)
	assertEngineOptimal(t, got, "ml")

	// Determinism: the identical request yields byte-identical JSON.
	if _, data2 := c.do("GET", "/games/g-ml/analysis", "", nil); !bytes.Equal(data, data2) {
		t.Error("two identical analysis requests returned different bytes")
	}

	// The champion seat, graded by the champion engine.
	resp2, data := c.do("GET", "/games/g-champ/analysis", "", nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("analysis (champion seat): %d %s", resp2.StatusCode, data)
	}
	got2 := decode[gameAnalysisV2Response](t, data)
	if got2.Seat != 1 {
		t.Fatalf("seat = %d, want 1", got2.Seat)
	}
	assertResponseInvariants(t, got2, events)
	assertEngineOptimal(t, got2, "champion")
}

// playOutGuestGame drives a live bot game to completion over HTTP, making
// champion moves for the token's seat (the server drives the bot seat). Used
// by the guest-access tests for both post-game endpoints, so the guest's own
// moves are champion-optimal by construction.
func playOutGuestGame(t *testing.T, c *authedClient, id, token string) {
	t.Helper()
	me := bot.Champion()
	for i := 0; ; i++ {
		if i > 500 {
			t.Fatal("game did not finish within 500 actions")
		}
		r, body := c.do("GET", "/games/"+id, token, nil)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("snapshot: %d %s", r.StatusCode, body)
		}
		v := decode[game.PlayerView](t, body)
		if v.Winner != nil {
			return
		}
		var act actionRequest
		switch {
		case v.Phase == game.PhaseDiscard && len(v.YourHand) == 6:
			cards := me.Discard(v)
			act = actionRequest{Type: "discard", Cards: cards[:]}
		case v.Phase == game.PhasePlay && v.ToPlay != nil && *v.ToPlay == v.You:
			card := me.Play(v)
			act = actionRequest{Type: "play", Card: &card}
		default:
			t.Fatalf("unexpected snapshot state: phase %v", v.Phase)
		}
		if r, body := c.do("POST", "/games/"+id+"/actions", token, act); r.StatusCode != http.StatusOK {
			t.Fatalf("action: %d %s", r.StatusCode, body)
		}
	}
}

// TestAnalysisV2GuestAccess plays a live bot game end-to-end over HTTP as a
// pure guest (no account, no cookie) making champion moves, then fetches the
// analysis with only the player token. This is the NU3 path: a fully-guest
// game never reaches the result store, so the live-session credential is the
// only way in — and the guest's seat, played by the champion, must analyze
// champion-optimal. Mid-game the same request is a 409 (post-game only).
func TestAnalysisV2GuestAccess(t *testing.T) {
	c, rs := newAuthedServer(t) // no signup: the client carries no login session

	resp, data := c.do("POST", "/games", "", createRequest{Mode: "bot", Bot: bot.ChampionName})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.StatusCode, data)
	}
	created := decode[createResponse](t, data)
	id, token := created.GameID, created.PlayerToken

	// Unfinished + valid participant token -> 409, not 404: analysis is
	// post-game only, but the token already proves participation.
	if r, body := c.do("GET", "/games/"+id+"/analysis", token, nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("mid-game analysis: %d %s, want 409", r.StatusCode, body)
	}

	// Play the game out with champion moves; the server drives the bot seat.
	playOutGuestGame(t, c, id, token)

	// A fully-guest game must NOT be in the permanent result store — the
	// live-session token path is what serves it.
	if _, ok, _ := rs.ResultByID(id); ok {
		t.Fatal("guest-vs-bot game unexpectedly reached the result store")
	}

	r, body := c.do("GET", "/games/"+id+"/analysis", token, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("guest analysis: %d %s", r.StatusCode, body)
	}
	got := decode[gameAnalysisV2Response](t, body)
	if got.GameID != id || got.Seat != 0 {
		t.Fatalf("game/seat = %s/%d, want %s/0", got.GameID, got.Seat, id)
	}
	if len(got.Deals) == 0 {
		t.Fatal("no deals analyzed")
	}
	// The guest's moves were the champion's, so the champion engine must
	// grade every one of them optimal — over the guest token path.
	assertEngineOptimal(t, got, "champion")

	// A token that opens no seat here -> non-revealing 404.
	if r, _ := c.do("GET", "/games/"+id+"/analysis", "bogus-token", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("bogus token: %d, want 404", r.StatusCode)
	}
	// No credential at all -> 401.
	if r, _ := c.do("GET", "/games/"+id+"/analysis", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("no credential: %d, want 401", r.StatusCode)
	}
}

// TestAnalysisV2AccessControl covers the remaining doors: a participant's
// happy path over the login credential, a logged-in spectator (404, never
// revealing the game exists), an unknown id (404), no credential (401), and
// a registered player's LIVE game (404 via the login path — no Result yet —
// but 409 via the player token, which proves participation).
func TestAnalysisV2AccessControl(t *testing.T) {
	c, rs := newAuthedServer(t)
	uid := c.signup("alice")

	h0 := mustHand(t, "5H 5S 5C 5D JH KH")
	h1 := mustHand(t, "2C 3C 4C 6C 7C 8C")
	rs.SaveResult(Result{
		ID:        "g1",
		PlayerIDs: [2]string{uid, "u2"},
		Events: []game.Event{
			game.HandDealt{Dealer: game.Seat0, Hands: [2][]cribbage.Card{h0, h1}},
			game.Discarded{Seat: game.Seat0, Cards: [2]cribbage.Card{h0[4], h0[5]}},
			game.Discarded{Seat: game.Seat1, Cards: [2]cribbage.Card{h1[4], h1[5]}},
			game.GameWon{Seat: game.Seat0},
		},
		EndedAt: time.Now(),
	})

	// Participant via login -> 200, analyzing a discard-only truncated game.
	r, body := c.do("GET", "/games/g1/analysis", "", nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("participant: %d %s", r.StatusCode, body)
	}
	got := decode[gameAnalysisV2Response](t, body)
	if got.Seat != 0 || len(got.Deals) != 1 || len(got.Deals[0].Plays) != 0 {
		t.Fatalf("unexpected shape: seat=%d deals=%d", got.Seat, len(got.Deals))
	}
	if s := got.Summary[engineIndex(t, got, "ml")]; s.Hands != 1 || s.PlayDecisions != 0 {
		t.Errorf("summary = %+v, want 1 hand, 0 play decisions", s)
	}
	// No pegging decisions: pegging_optimal is vacuously true for every engine.
	for i, ru := range got.Deals[0].Rollup {
		if !ru.PeggingOptimal {
			t.Errorf("rollup[%d].pegging_optimal = false, want vacuously true", i)
		}
	}

	// A logged-in user who was NOT a participant -> 404 (never reveal).
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	c2 := &authedClient{t: t, ts: c.ts, cli: &http.Client{Jar: jar}}
	c2.signup("bob")
	if r, _ := c2.do("GET", "/games/g1/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("spectator: %d, want 404", r.StatusCode)
	}

	// Unknown id -> 404; no credential -> 401.
	if r, _ := c.do("GET", "/games/nope/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id: %d, want 404", r.StatusCode)
	}
	plain := &http.Client{}
	req, _ := http.NewRequest("GET", c.ts.URL+"/games/g1/analysis", nil)
	r2, err := plain.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized {
		t.Errorf("no credential: %d, want 401", r2.StatusCode)
	}

	// A registered player's LIVE game: via the login path there is no stored
	// Result yet -> 404 (v1's convention); via the player token the game's
	// existence is already known to the caller -> 409 until it finishes.
	cr, crData := c.do("POST", "/games", "", createRequest{Mode: "bot"})
	if cr.StatusCode != http.StatusCreated {
		t.Fatalf("create live game: %d %s", cr.StatusCode, crData)
	}
	created := decode[createResponse](t, crData)
	if r, _ := c.do("GET", "/games/"+created.GameID+"/analysis", "", nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("live game via login: %d, want 404", r.StatusCode)
	}
	if r, _ := c.do("GET", "/games/"+created.GameID+"/analysis", created.PlayerToken, nil); r.StatusCode != http.StatusConflict {
		t.Errorf("live game via token: %d, want 409", r.StatusCode)
	}
}
