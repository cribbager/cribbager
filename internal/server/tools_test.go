package server

import (
	"net/http"
	"testing"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// TestDiscardEvalRanking checks the tool returns the full 15-hold ranking for a
// known hand, best-first, matching eval.RankDiscards (its single source of truth),
// and that the EV breakdown sums correctly.
func TestDiscardEvalRanking(t *testing.T) {
	c := newTestClient(t)

	resp, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, "5H 5S 5C 5D JH KH"), Dealer: true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, data)
	}
	out := decode[discardEvalResponse](t, data)
	if len(out.Holds) != 15 {
		t.Fatalf("want 15 holds, got %d", len(out.Holds))
	}
	if !out.Dealer {
		t.Fatalf("dealer flag not echoed")
	}

	// The top hold must match eval's own best discard (no scoring duplicated here).
	want := eval.RankDiscards(hand6(t, "5H 5S 5C 5D JH KH"), true)
	if out.Holds[0].Throw != want[0].Discard || out.Holds[0].Keep != want[0].Keep {
		t.Fatalf("top hold = throw %v keep %v, want throw %v keep %v",
			out.Holds[0].Throw, out.Holds[0].Keep, want[0].Discard, want[0].Keep)
	}
	// Best-first, and EV = hand_ev + crib_ev (within rounding).
	for i := 1; i < len(out.Holds); i++ {
		if out.Holds[i-1].EV < out.Holds[i].EV {
			t.Fatalf("holds not sorted best-first at %d: %v > %v", i, out.Holds[i].EV, out.Holds[i-1].EV)
		}
	}
	for i, hld := range out.Holds {
		if got := round4(hld.HandEV + hld.CribEV); got != hld.EV {
			t.Fatalf("hold %d: hand_ev+crib_ev=%v, ev=%v", i, got, hld.EV)
		}
	}
}

// TestDiscardEvalDealerFlips checks that whose crib it is changes the EV (the crib
// term flips sign), so the dealer flag is genuinely consumed by the evaluator.
func TestDiscardEvalDealerFlips(t *testing.T) {
	c := newTestClient(t)
	hand := mustHand(t, "5H 5S 6C 6D JH KS")

	_, d1 := c.do("POST", "/tools/discard-eval", "", discardEvalRequest{Hand: hand, Dealer: true})
	_, d2 := c.do("POST", "/tools/discard-eval", "", discardEvalRequest{Hand: hand, Dealer: false})
	dealer := decode[discardEvalResponse](t, d1)
	pone := decode[discardEvalResponse](t, d2)

	// Same hand: the hold set is identical, but the crib term's sign flips, so the
	// best-EV figure must differ between owning and not owning the crib.
	if dealer.Holds[0].EV == pone.Holds[0].EV {
		t.Fatalf("dealer/pone produced the same best EV (%v); the crib sign did not flip", dealer.Holds[0].EV)
	}
}

// TestDiscardEvalBadInput checks strict validation: wrong card count, duplicate
// cards, and a malformed card string all yield 400.
func TestDiscardEvalBadInput(t *testing.T) {
	c := newTestClient(t)
	cases := []struct {
		name string
		body string
	}{
		{"too few", `{"hand":["5H","5S","5C","5D","JH"],"dealer":true}`},
		{"too many", `{"hand":["5H","5S","5C","5D","JH","KH","2C"],"dealer":true}`},
		{"duplicate", `{"hand":["5H","5H","5C","5D","JH","KH"],"dealer":true}`},
		{"malformed card", `{"hand":["5H","5S","5C","5D","JH","ZZ"],"dealer":true}`},
		{"not a card", `{"hand":["5H","5S","5C","5D","JH","10"],"dealer":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := c.do("POST", "/tools/discard-eval", "", rawBody(tc.body))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", resp.StatusCode, data)
			}
		})
	}
}

// scoreHand4 parses exactly four cards for a score-hand request body.
func scoreHand4(t *testing.T, s string) []cribbage.Card {
	t.Helper()
	cards := mustHand(t, s)
	if len(cards) != 4 {
		t.Fatalf("want 4 cards, got %d", len(cards))
	}
	return cards
}

func mustCard(t *testing.T, s string) cribbage.Card {
	t.Helper()
	c, err := cribbage.ParseCard(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return c
}

// findCombo returns the first combo of the given kind, or nil.
func findCombo(combos []scoredCombo, kind string) *scoredCombo {
	for i := range combos {
		if combos[i].Kind == kind {
			return &combos[i]
		}
	}
	return nil
}

// TestScoreHandKnownHands grades several teaching hands against the engine's own
// hand.Score (its single source of truth): the legendary 29, a double run, a hand
// flush (4) vs the same cards as a crib (0), a five-card flush (5), and his nobs.
// Every case also cross-checks that the returned combos sum to the total.
func TestScoreHandKnownHands(t *testing.T) {
	c := newTestClient(t)

	cases := []struct {
		name      string
		hand      string
		starter   string
		crib      bool
		wantTotal int
		check     func(t *testing.T, out scoreHandResponse)
	}{
		{
			name: "perfect 29", hand: "5C 5D 5H JS", starter: "5S", wantTotal: 29,
		},
		{
			name: "double run", hand: "5H 6H 7S 7D", starter: "8C",
			// run 5-6-7-8 doubled, points absorbing its pair = 4*2+2 = 10, plus two
			// fifteens (7+8 twice) = 4, for 14 total.
			wantTotal: 14,
			check: func(t *testing.T, out scoreHandResponse) {
				run := findCombo(out.Combos, "run")
				if run == nil {
					t.Fatalf("no run combo: %+v", out.Combos)
				}
				// The double run is ONE combo: run_length 4, multiplicity 2, and its
				// points already include the absorbed pair (no standalone pair combo).
				if run.RunLength != 4 || run.Multiplicity != 2 || run.Points != 10 {
					t.Fatalf("run combo = len %d mult %d pts %d, want 4/2/10", run.RunLength, run.Multiplicity, run.Points)
				}
				if findCombo(out.Combos, "pair") != nil {
					t.Fatalf("pair should be absorbed into the run, got %+v", out.Combos)
				}
			},
		},
		{
			name: "hand flush (4)", hand: "2H 4H 6H 8H", starter: "KS", wantTotal: 4,
			check: func(t *testing.T, out scoreHandResponse) {
				if f := findCombo(out.Combos, "flush"); f == nil || f.Points != 4 {
					t.Fatalf("want flush 4, got %+v", out.Combos)
				}
			},
		},
		{
			name: "crib flush needs all five", hand: "2H 4H 6H 8H", starter: "KS", crib: true, wantTotal: 0,
			check: func(t *testing.T, out scoreHandResponse) {
				if f := findCombo(out.Combos, "flush"); f != nil {
					t.Fatalf("crib should not flush on four matching suits, got %+v", f)
				}
			},
		},
		{
			name: "five-card flush (5)", hand: "2H 4H 6H 8H", starter: "TH", crib: true, wantTotal: 5,
			check: func(t *testing.T, out scoreHandResponse) {
				if f := findCombo(out.Combos, "flush"); f == nil || f.Points != 5 {
					t.Fatalf("want flush 5, got %+v", out.Combos)
				}
			},
		},
		{
			name: "his nobs", hand: "JD 2C 4S 6H", starter: "8D", wantTotal: 1,
			check: func(t *testing.T, out scoreHandResponse) {
				if n := findCombo(out.Combos, "nobs"); n == nil || n.Points != 1 {
					t.Fatalf("want nobs 1, got %+v", out.Combos)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := scoreHandRequest{Hand: scoreHand4(t, tc.hand), Starter: mustCard(t, tc.starter), Crib: tc.crib}
			resp, data := c.do("POST", "/tools/score-hand", "", body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status: %d %s", resp.StatusCode, data)
			}
			out := decode[scoreHandResponse](t, data)

			if out.Total != tc.wantTotal {
				t.Fatalf("total = %d, want %d", out.Total, tc.wantTotal)
			}
			// Cross-check against the engine directly (no duplicated rules here).
			var hc [4]cribbage.Card
			copy(hc[:], body.Hand)
			want, err := hand.Total(hc, body.Starter, tc.crib)
			if err != nil {
				t.Fatalf("hand.Total: %v", err)
			}
			if out.Total != want {
				t.Fatalf("total = %d, engine = %d", out.Total, want)
			}
			// The itemized combos must sum to the total.
			sum := 0
			for _, cb := range out.Combos {
				sum += cb.Points
			}
			if sum != out.Total {
				t.Fatalf("combos sum to %d, total %d (%+v)", sum, out.Total, out.Combos)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}
}

// TestScoreHandBadInput checks strict validation: wrong card count, a missing
// starter, duplicate cards across hand+starter, and malformed cards all yield 400.
func TestScoreHandBadInput(t *testing.T) {
	c := newTestClient(t)
	cases := []struct {
		name string
		body string
	}{
		{"too few", `{"hand":["5H","5S","5C"],"starter":"5D"}`},
		{"too many", `{"hand":["5H","5S","5C","5D","JH"],"starter":"6C"}`},
		{"missing starter", `{"hand":["5H","5S","5C","5D"]}`},
		{"duplicate in hand", `{"hand":["5H","5H","5C","5D"],"starter":"6C"}`},
		{"starter duplicates hand", `{"hand":["5H","5S","5C","5D"],"starter":"5H"}`},
		{"malformed card", `{"hand":["5H","5S","5C","ZZ"],"starter":"6C"}`},
		{"not a card", `{"hand":["5H","5S","5C","10"],"starter":"6C"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, data := c.do("POST", "/tools/score-hand", "", rawBody(tc.body))
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("want 400, got %d (%s)", resp.StatusCode, data)
			}
		})
	}
}

// rawBody lets c.do send a precomposed JSON string verbatim (it json.Marshals the
// body, and a string marshals to a quoted string — so we wrap it to emit raw bytes
// via json.RawMessage).
func rawBody(s string) any { return rawJSON(s) }

type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }
