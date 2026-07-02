package lab

import (
	"fmt"
	"os"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
)

// TestChallengerVsChampion is the promotion gate: it evaluates a lab challenger
// against the shipped champion over duplicate deals (card luck cancelled) and
// logs both paired instruments with 95% CIs. The two-instrument rule:
//
//   - point-EV changes (better discards, better pegging EV) gate on the points
//     Margin CI clearing zero;
//   - score-aware changes (win-probability play) gate on the WinDiff CI clearing
//     zero, because they legitimately trade points for wins and the points
//     instrument is structurally blind to that;
//   - either way, the OTHER instrument must not be significantly negative.
//
// Power note: WinDiff per pair has SD ≤ ~0.7, so at 5000 pairs the CI half-width
// is ~±0.019 (≈2 percentage points of win rate). Full-game WinDiff only sees
// large edges; endgame-focused changes should lean on the positional fixtures
// (fixtures_test.go), where the decisions concentrate.
//
// It is OPT-IN — a normal `go test ./...` skips it, since a high-confidence
// tournament is slow. Run it explicitly, naming the registered challenger:
//
//	CHALLENGE=candidate go test ./internal/bot/lab -run ChallengerVsChampion -v
//
// PAIRS overrides the deal-pair count (default 2000); each pair is two games.
func TestChallengerVsChampion(t *testing.T) {
	name := os.Getenv("CHALLENGE")
	if name == "" {
		t.Skip("set CHALLENGE=<challenger name> to evaluate a challenger vs the champion")
	}
	cand, ok := New(name)
	if !ok {
		t.Fatalf("no lab challenger %q (have %v)", name, Names())
	}

	pairs := 2000
	if v := os.Getenv("PAIRS"); v != "" {
		if _, err := fmt.Sscan(v, &pairs); err != nil {
			t.Fatalf("PAIRS: %q is not a number", v)
		}
	}

	c, err := bot.Compare(cand, bot.Champion(), pairs, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("\n%s vs %s — %d deal-pairs (%d games)\n"+
		"  %s win rate  : %.1f%%\n"+
		"  avg score    : %s %.1f — %.1f %s\n"+
		"  paired margin : %+.2f pts/pair    95%% CI [%+.2f, %+.2f]\n"+
		"  paired windiff: %+.3f wins/pair   95%% CI [%+.3f, %+.3f]   (A-both %d / split %d / B-both %d)%s",
		c.A, c.B, c.Pairs, c.Games,
		c.A, 100*c.AWinRate,
		c.A, c.AAvgScore, c.BAvgScore, c.B,
		c.Margin, c.MarginCILo, c.MarginCIHi,
		c.WinDiff, c.WinDiffCILo, c.WinDiffCIHi,
		c.PairsAWinBoth, c.PairsSplit, c.PairsBWinBoth, verdict(c))
}

// verdict reads the two paired CIs into a promotion call, per the
// two-instrument rule documented on the test.
func verdict(c bot.Comparison) string {
	marginNeg := c.MarginCIHi < 0
	winNeg := c.WinDiffCIHi < 0
	switch {
	case c.MarginCILo > 0 && !winNeg:
		return "\n  → challenger is BETTER on points (Margin CI clears zero, wins not worse) — promote, then delete the challenger."
	case c.WinDiffCILo > 0 && !marginNeg:
		return "\n  → challenger is BETTER on wins (WinDiff CI clears zero, points not worse) — promote, then delete the challenger."
	case c.WinDiffCILo > 0 && marginNeg:
		return "\n  → challenger WINS MORE but scores significantly fewer points — expected for score-aware play; promote if the point loss is understood."
	case marginNeg || winNeg:
		return "\n  → challenger is WORSE — delete it."
	default:
		return "\n  → no significant difference (both CIs span zero) — delete it; bolt-on tweaks don't pay."
	}
}
