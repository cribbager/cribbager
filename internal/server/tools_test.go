package server

import (
	"net/http"
	"testing"

	"github.com/cribbager/cribbager/internal/bot/eval"
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

// rawBody lets c.do send a precomposed JSON string verbatim (it json.Marshals the
// body, and a string marshals to a quoted string — so we wrap it to emit raw bytes
// via json.RawMessage).
func rawBody(s string) any { return rawJSON(s) }

type rawJSON string

func (r rawJSON) MarshalJSON() ([]byte, error) { return []byte(r), nil }
