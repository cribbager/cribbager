package eval

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// TestPeggingHeuristicsAudit checks the champion's play policy against the
// classic expert pegging heuristics (docs/research/strategy-synthesis.md,
// experts.md: Colvert, Schell, ACC tips). It is an AUDIT, not a gate: the bot
// maximizes one-ply net EV and the heuristics are human rules of thumb, so a
// disagreement is either a bot leak or a heuristic myth — both worth knowing.
// The test fails only on structural problems (an illegal or missing choice);
// the agreement table lands in the log:
//
//	go test ./internal/bot/eval -run PeggingHeuristicsAudit -v
//
// A DISAGREE with a large EV gap in the heuristic's favor is a candidate lab
// challenger; a near-zero gap means the choice barely matters.
func TestPeggingHeuristicsAudit(t *testing.T) {
	cs := func(names ...string) []cribbage.Card {
		out := make([]cribbage.Card, len(names))
		for i, n := range names {
			out[i] = card(t, n)
		}
		return out
	}

	cases := []struct {
		name      string
		heuristic string
		hand      []string // my unplayed cards (LegalPlays = playable subset)
		myPlayed  []string
		oppPlayed []string
		pile      []string // live series, in play order
		want      []string // the heuristic's acceptable choices
	}{
		{
			name:      "never lead a 5",
			heuristic: "a 5 lead lets any ten-card make 15; lead anything else",
			hand:      []string{"5H", "9C", "QD", "KS"},
			want:      []string{"9C", "QD", "KS"},
		},
		{
			name:      "lead low and safe",
			heuristic: "a lead of 4 or under cannot be fifteened by one card",
			hand:      []string{"4H", "9D", "KC", "7S"},
			want:      []string{"4H"},
		},
		{
			name:      "lead from a pair",
			heuristic: "lead one of a pair: if the opponent pairs you, you triple",
			hand:      []string{"7H", "7D", "KC", "2S"},
			want:      []string{"7H", "7D", "2S"}, // 2S allowed: the safe-lead rule competes
		},
		{
			name:      "don't make the count 5",
			heuristic: "a count of 5 lets any ten-card make 15",
			hand:      []string{"3D", "8C", "JD", "AS"},
			oppPlayed: []string{"2H"},
			pile:      []string{"2H"},
			want:      []string{"8C", "JD", "AS"},
		},
		{
			name:      "don't make the count 21",
			heuristic: "a count of 21 lets any ten-card make 31",
			hand:      []string{"7D", "6S", "3C", "AC"},
			oppPlayed: []string{"9H", "5C"},
			pile:      []string{"9H", "5C"},
			want:      []string{"6S", "3C", "AC"},
		},
		{
			name:      "take the sure 15 over a risky pair setup",
			heuristic: "score guaranteed points now rather than invite a triple",
			hand:      []string{"5D", "TC", "4S", "9H"},
			oppPlayed: []string{"TH"},
			pile:      []string{"TH"},
			want:      []string{"5D"}, // 15 for 2; TC pairs but invites trips
		},
		{
			name:      "avoid feeding a run",
			heuristic: "playing next to the opponent's card invites a 3-card run",
			hand:      []string{"4D", "KC", "9S", "TS"},
			oppPlayed: []string{"3H"},
			pile:      []string{"3H"},
			want:      []string{"KC", "9S", "TS"},
		},
		{
			name:      "keep a low card for the go",
			heuristic: "with the count high, dump the big card and keep 31-reach",
			hand:      []string{"2D", "TC"},
			myPlayed:  []string{"8H", "7S"},
			oppPlayed: []string{"KH", "9D"},
			pile:      []string{"KH", "9D"}, // count 19: TC makes 29, 2D makes 21
			want:      []string{"TC"},
		},
	}

	var report []string
	for _, tc := range cases {
		hand := cs(tc.hand...)
		pile := cs(tc.pile...)
		count := 0
		for _, c := range pile {
			count += c.Rank.PipValue()
		}
		var legal []cribbage.Card
		for _, c := range hand {
			if count+c.Rank.PipValue() <= 31 {
				legal = append(legal, c)
			}
		}
		v := game.PlayerView{
			Phase:          game.PhasePlay,
			YourHand:       hand,
			YourPlayed:     cs(tc.myPlayed...),
			OpponentPlayed: cs(tc.oppPlayed...),
			Pile:           pile,
			Count:          count,
			LegalPlays:     legal,
		}

		ranked := RankPlays(v)
		if len(ranked) == 0 {
			t.Errorf("%s: no ranked plays", tc.name)
			continue
		}
		got := ranked[0]
		inLegal := false
		for _, c := range legal {
			if c == got.Card {
				inLegal = true
			}
		}
		if !inLegal {
			t.Errorf("%s: chose %s, not a legal play", tc.name, got.Card)
			continue
		}

		agrees := false
		for _, w := range tc.want {
			if got.Card == card(t, w) {
				agrees = true
			}
		}
		if agrees {
			report = append(report, "AGREE    "+tc.name+": bot plays "+got.Card.String())
			continue
		}
		// The heuristic's best-ranked alternative, and how much EV the bot
		// thinks it gives up — the size of the disagreement.
		bestAltGap := -1.0
		bestAlt := ""
		for _, rp := range ranked {
			for _, w := range tc.want {
				if rp.Card == card(t, w) && bestAltGap < 0 {
					bestAltGap = got.Score - rp.Score
					bestAlt = rp.Card.String()
				}
			}
		}
		report = append(report, fmt.Sprintf(
			"DISAGREE %s: bot plays %s, heuristic wants %s (best: %s, bot sees it as %.2f pts worse)",
			tc.name, got.Card, strings.Join(tc.want, "/"), bestAlt, bestAltGap))
	}
	t.Log("\npegging heuristics audit:\n  " + strings.Join(report, "\n  "))
}
