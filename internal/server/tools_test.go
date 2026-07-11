package server

import (
	"net/http"
	"strings"
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
		{"my_score without opp_score", `{"hand":["5H","5S","5C","5D","JH","KH"],"dealer":true,"my_score":90}`},
		{"opp_score without my_score", `{"hand":["5H","5S","5C","5D","JH","KH"],"dealer":true,"opp_score":90}`},
		{"score negative", `{"hand":["5H","5S","5C","5D","JH","KH"],"dealer":true,"my_score":-1,"opp_score":90}`},
		{"score past 120", `{"hand":["5H","5S","5C","5D","JH","KH"],"dealer":true,"my_score":90,"opp_score":121}`},
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

func intp(v int) *int { return &v }

// TestDiscardEvalHandDistribution checks the additive upside block: every hold
// carries a length-30 hand-score distribution that sums to ~1, whose mean equals
// the hold's hand_ev (the histogram is the same sweep, un-collapsed), and whose
// derived summaries (P≥12, p90, ceiling) agree with the raw histogram.
func TestDiscardEvalHandDistribution(t *testing.T) {
	c := newTestClient(t)
	const handStr = "5H 5S 5C JH 2D 3C" // lets 5-5-5-J be one of the 15 holds
	resp, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, handStr), Dealer: true})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, data)
	}
	out := decode[discardEvalResponse](t, data)
	if len(out.Holds) != 15 {
		t.Fatalf("want 15 holds, got %d", len(out.Holds))
	}

	for i, hld := range out.Holds {
		if len(hld.HandDist) != eval.HandScoreDistSize {
			t.Fatalf("hold %d: dist length %d, want %d", i, len(hld.HandDist), eval.HandScoreDistSize)
		}
		// Probabilities sum to ~1 (per-entry rounding leaves a tiny residual).
		sum, mean, tail := 0.0, 0.0, 0.0
		ceiling := 0
		for s, p := range hld.HandDist {
			if p < 0 {
				t.Fatalf("hold %d: negative probability %v at score %d", i, p, s)
			}
			sum += p
			mean += float64(s) * p
			if s >= bigHandThreshold {
				tail += p
			}
			if p > 0 {
				ceiling = s
			}
		}
		if sum < 0.999 || sum > 1.001 {
			t.Fatalf("hold %d: distribution sums to %v, want ~1", i, sum)
		}
		// The histogram is HandEV kept un-collapsed, so its mean must recover HandEV.
		if diff := mean - hld.HandEV; diff > 2e-3 || diff < -2e-3 {
			t.Fatalf("hold %d: dist mean %v vs hand_ev %v", i, mean, hld.HandEV)
		}
		// The derived summaries must match the raw histogram.
		if diff := tail - hld.HandPGE12; diff > 1e-4 || diff < -1e-4 {
			t.Fatalf("hold %d: recomputed P(>=12) %v vs hand_p_ge_12 %v", i, tail, hld.HandPGE12)
		}
		if hld.HandCeiling != ceiling {
			t.Fatalf("hold %d: ceiling %d, raw histogram %d", i, hld.HandCeiling, ceiling)
		}
		// p90 is the smallest score with cumulative probability >= 0.90.
		cum, wantP90 := 0.0, -1
		for s, p := range hld.HandDist {
			cum += p
			if cum >= 0.90 {
				wantP90 = s
				break
			}
		}
		if hld.HandP90 != wantP90 {
			t.Fatalf("hold %d: p90 %d, want %d", i, hld.HandP90, wantP90)
		}
		// Sanity: a summary never exceeds the ceiling.
		if hld.HandP90 > hld.HandCeiling {
			t.Fatalf("hold %d: p90 %d above ceiling %d", i, hld.HandP90, hld.HandCeiling)
		}
	}
}

// TestDiscardEvalUpsideShape checks the pedagogical claim: the 5-5-5-J keep (three
// fives + a jack) has a fat right tail — a real chance of a big hand and a high
// ceiling — that a flat, safe keep from the same deal does not.
func TestDiscardEvalUpsideShape(t *testing.T) {
	c := newTestClient(t)
	const handStr = "5H 5S 5C JH 2D 3C"
	_, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, handStr), Dealer: true})
	out := decode[discardEvalResponse](t, data)

	find := func(keep string) rankedHold {
		cards := mustHand(t, keep)
		var set [4]cribbage.Card
		copy(set[:], cards)
		for _, hld := range out.Holds {
			match := 0
			for _, k := range hld.Keep {
				for _, w := range set {
					if k == w {
						match++
					}
				}
			}
			if match == 4 {
				return hld
			}
		}
		t.Fatalf("keep %q not among holds", keep)
		return rankedHold{}
	}

	fat := find("5H 5S 5C JH")  // three fives + jack: pairs royal + fifteens, tens cut for more
	flat := find("2D 3C 5H JH") // a scattered, low-upside keep from the same deal

	if fat.HandPGE12 <= flat.HandPGE12 {
		t.Fatalf("expected 5-5-5-J P(>=12) %v to exceed flat keep %v", fat.HandPGE12, flat.HandPGE12)
	}
	if fat.HandCeiling <= flat.HandCeiling {
		t.Fatalf("expected 5-5-5-J ceiling %d to exceed flat keep %d", fat.HandCeiling, flat.HandCeiling)
	}
	if fat.HandPGE12 <= 0 {
		t.Fatalf("5-5-5-J should have real right-tail mass, got P(>=12)=%v", fat.HandPGE12)
	}
}

// TestDiscardEvalDistAlwaysPresent checks the numeric-fields-always-present
// convention holds for the distribution block: hand_dist, hand_p_ge_12, hand_p90,
// and hand_ceiling appear on every hold even for a scoreless request.
func TestDiscardEvalDistAlwaysPresent(t *testing.T) {
	c := newTestClient(t)
	_, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, "2C 5H 6D 9S JD QH"), Dealer: false})
	for _, field := range []string{`"hand_dist"`, `"hand_p_ge_12"`, `"hand_p90"`, `"hand_ceiling"`} {
		if got := strings.Count(string(data), field); got != 15 {
			t.Fatalf("want %s on all 15 holds, found %d (%s)", field, got, data)
		}
	}
}

// TestDiscardEvalNoScoresShape checks the scoreless request keeps its original
// wire shape plus the always-present win field: no situation block, and win
// serialized as a literal 0 on every hold (numeric fields are never omitempty —
// see TestDeltaNumericFieldsAlwaysPresent for the convention).
func TestDiscardEvalNoScoresShape(t *testing.T) {
	c := newTestClient(t)
	resp, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, "2C 5H 6D 9S JD QH"), Dealer: false})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, data)
	}
	if strings.Contains(string(data), `"situation"`) {
		t.Fatalf("situation block present without scores: %s", data)
	}
	if got := strings.Count(string(data), `"win":0`); got != 15 {
		t.Fatalf(`want "win":0 on all 15 holds, found %d (%s)`, got, data)
	}
}

// TestDiscardEvalScoresFarFromEnd checks a scored request at 0–0: the situation
// block reports the opening position with the endgame objective OFF, every win
// is 0 (the win ranking defers to points EV), and all three lenses are derivable
// from the one response — point EV is the response order, max-hand re-sorts by
// hand_ev, and the win lens is documented as deferring rather than ranking.
func TestDiscardEvalScoresFarFromEnd(t *testing.T) {
	c := newTestClient(t)
	const handStr = "2C 5H 6D 9S JD QH"
	resp, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, handStr), Dealer: true, MyScore: intp(0), OppScore: intp(0)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, data)
	}
	out := decode[discardEvalResponse](t, data)

	sit := out.Situation
	if sit == nil {
		t.Fatalf("no situation block: %s", data)
	}
	if sit.MyScore != 0 || sit.OppScore != 0 || sit.MyNeed != 121 || sit.OppNeed != 121 {
		t.Fatalf("situation scores/needs wrong: %+v", sit)
	}
	if sit.Endgame {
		t.Fatalf("endgame objective active at 0-0: %+v", sit)
	}
	if want := round4(eval.WinProb(0, 0, true)); sit.WinProb != want {
		t.Fatalf("win_prob = %v, want %v", sit.WinProb, want)
	}

	// Far from the end the win ranking defers to points EV: the hold order must
	// match eval.RankDiscards exactly and every win must be 0.
	want := eval.RankDiscards(hand6(t, handStr), true)
	for i, hld := range out.Holds {
		if hld.Throw != want[i].Discard || hld.Keep != want[i].Keep {
			t.Fatalf("hold %d = throw %v keep %v, want throw %v keep %v",
				i, hld.Throw, hld.Keep, want[i].Discard, want[i].Keep)
		}
		if hld.Win != 0 {
			t.Fatalf("hold %d has win %v at 0-0", i, hld.Win)
		}
	}

	// The max-hand lens is a pure re-sort of the same rows by hand_ev: its top
	// must carry the maximum EHand among all 15 holds.
	bestHand := out.Holds[0].HandEV
	for _, hld := range out.Holds {
		if hld.HandEV > bestHand {
			bestHand = hld.HandEV
		}
	}
	wantBestHand := round4(want[0].EHand)
	for _, rd := range want {
		if v := round4(rd.EHand); v > wantBestHand {
			wantBestHand = v
		}
	}
	if bestHand != wantBestHand {
		t.Fatalf("max hand_ev = %v, engine max EHand = %v", bestHand, wantBestHand)
	}
}

// TestDiscardEvalScoresEndgame checks a scored request in reach of 121 (behind
// 90–117, opponent deals): the endgame objective is ON, the holds arrive in
// eval.RankDiscardsWin's win-probability order with differentiated win values,
// and the situation block matches eval.WinProb.
func TestDiscardEvalScoresEndgame(t *testing.T) {
	c := newTestClient(t)
	const handStr = "4H 5D 6S 6C TD JC"
	resp, data := c.do("POST", "/tools/discard-eval", "",
		discardEvalRequest{Hand: mustHand(t, handStr), Dealer: false, MyScore: intp(90), OppScore: intp(117)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, data)
	}
	out := decode[discardEvalResponse](t, data)

	sit := out.Situation
	if sit == nil {
		t.Fatalf("no situation block: %s", data)
	}
	if !sit.Endgame {
		t.Fatalf("endgame objective not active at 90-117: %+v", sit)
	}
	if sit.MyNeed != 31 || sit.OppNeed != 4 {
		t.Fatalf("needs = %d/%d, want 31/4", sit.MyNeed, sit.OppNeed)
	}
	if want := round4(eval.WinProb(90, 117, false)); sit.WinProb != want {
		t.Fatalf("win_prob = %v, want %v", sit.WinProb, want)
	}

	// The hold order is eval.RankDiscardsWin's (win objective, EV near-ties) —
	// the single source of truth, compared hold by hold.
	want := eval.RankDiscardsWin(hand6(t, handStr), false, 90, 117)
	for i, hld := range out.Holds {
		if hld.Throw != want[i].Discard || hld.Keep != want[i].Keep {
			t.Fatalf("hold %d = throw %v keep %v, want throw %v keep %v",
				i, hld.Throw, hld.Keep, want[i].Discard, want[i].Keep)
		}
		if hld.Win != round6(want[i].Win) {
			t.Fatalf("hold %d win = %v, want %v", i, hld.Win, round6(want[i].Win))
		}
	}
	// The win lens must actually differentiate here: a positive best and a real
	// spread across the 15 holds (otherwise the lens is indistinguishable from EV).
	if out.Holds[0].Win <= 0 {
		t.Fatalf("best hold win = %v, want > 0", out.Holds[0].Win)
	}
	if out.Holds[0].Win == out.Holds[len(out.Holds)-1].Win {
		t.Fatalf("win identical across all holds (%v); expected a spread", out.Holds[0].Win)
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
