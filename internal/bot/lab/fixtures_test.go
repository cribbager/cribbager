package lab

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/game"
)

// fixtures are the preset positions where score-aware play concentrates.
// Because the challenger and champion swap seats within every duplicate pair,
// fixture (scores [a,b], dealer Seat0) produces the very same games as
// (scores [b,a], dealer Seat1) — so only canonical forms appear here, written
// as dealer-score vs pone-score with Seat0 dealing. Each fixture also gets its
// own deal-seed range (below), so their samples are independent and the pooled
// CI is honest.
var fixtures = []struct {
	name  string
	start game.Start
}{
	{"even 115-115", game.Start{Scores: [2]int{115, 115}, Dealer: game.Seat0}},
	{"even 118-118", game.Start{Scores: [2]int{118, 118}, Dealer: game.Seat0}},
	{"dealer ahead 100-90", game.Start{Scores: [2]int{100, 90}, Dealer: game.Seat0}},
	{"dealer behind 90-100", game.Start{Scores: [2]int{90, 100}, Dealer: game.Seat0}},
	{"dealer ahead 105-95", game.Start{Scores: [2]int{105, 95}, Dealer: game.Seat0}},
	{"dealer behind 95-105", game.Start{Scores: [2]int{95, 105}, Dealer: game.Seat0}},
	{"dealer behind 108-115", game.Start{Scores: [2]int{108, 115}, Dealer: game.Seat0}},
	{"even 60-60", game.Start{Scores: [2]int{60, 60}, Dealer: game.Seat0}},
}

// TestChallengerFixtures evaluates a lab challenger against the champion from
// each preset position — the high-power instrument for score-aware changes,
// which are diluted to invisibility in full games from 0–0. Judge by the
// per-fixture WinDiff CIs plus the pooled verdict; a score-aware challenger
// should be non-negative everywhere and clearly positive on the endgame
// fixtures.
//
// OPT-IN like the main gate:
//
//	CHALLENGE=candidate go test ./internal/bot/lab -run ChallengerFixtures -v
//
// PAIRS overrides the per-fixture deal-pair count (default 1000).
func TestChallengerFixtures(t *testing.T) {
	name := os.Getenv("CHALLENGE")
	if name == "" {
		t.Skip("set CHALLENGE=<challenger name> to evaluate a challenger vs the champion")
	}
	cand, ok := New(name)
	if !ok {
		t.Fatalf("no lab challenger %q (have %v)", name, Names())
	}

	pairs := 1000
	if v := os.Getenv("PAIRS"); v != "" {
		if _, err := fmt.Sscan(v, &pairs); err != nil {
			t.Fatalf("PAIRS: %q is not a number", v)
		}
	}

	// Pool the fixtures as one big paired sample for the overall verdict.
	var pooledDiff, pooledVar float64
	for fi, f := range fixtures {
		c, err := bot.CompareFrom(cand, bot.Champion(), pairs, int64(1+fi*pairs), f.start)
		if err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		t.Logf("%-24s windiff %+.3f  95%% CI [%+.3f, %+.3f]   (A-both %d / split %d / B-both %d)",
			f.name, c.WinDiff, c.WinDiffCILo, c.WinDiffCIHi,
			c.PairsAWinBoth, c.PairsSplit, c.PairsBWinBoth)
		se := (c.WinDiffCIHi - c.WinDiff) / 1.96
		pooledDiff += c.WinDiff
		pooledVar += se * se
	}
	n := float64(len(fixtures))
	pooledDiff /= n
	pooledSE := math.Sqrt(pooledVar) / n
	lo, hi := pooledDiff-1.96*pooledSE, pooledDiff+1.96*pooledSE
	t.Logf("%-24s windiff %+.3f  95%% CI [%+.3f, %+.3f]%s",
		"POOLED", pooledDiff, lo, hi, fixtureVerdict(lo, hi))
}

func fixtureVerdict(lo, hi float64) string {
	switch {
	case lo > 0:
		return "\n  → challenger WINS MORE from the fixture positions."
	case hi < 0:
		return "\n  → challenger wins LESS from the fixture positions."
	default:
		return "\n  → no significant difference across fixtures."
	}
}
