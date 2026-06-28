package lab

import (
	"fmt"
	"os"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
)

// TestChallengerVsChampion is the promotion gate: it evaluates a lab challenger
// against the shipped champion over duplicate deals (card luck cancelled) and
// logs the paired margin + 95% CI. Promote the challenger only when the CI clears
// zero; otherwise delete it.
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
		"  %s win rate : %.1f%%\n"+
		"  avg score   : %s %.1f — %.1f %s\n"+
		"  paired margin: %+.2f pts/pair   95%% CI [%+.2f, %+.2f]%s",
		c.A, c.B, c.Pairs, c.Games,
		c.A, 100*c.AWinRate,
		c.A, c.AAvgScore, c.BAvgScore, c.B,
		c.Margin, c.MarginCILo, c.MarginCIHi, verdict(c))
}

// verdict reads the paired-margin CI into a promotion call.
func verdict(c bot.Comparison) string {
	switch {
	case c.MarginCILo > 0:
		return "\n  → challenger is BETTER (CI clears zero) — promote, then delete the challenger."
	case c.MarginCIHi < 0:
		return "\n  → challenger is WORSE — delete it."
	default:
		return "\n  → no significant difference (CI spans zero) — delete it; bolt-on tweaks don't pay."
	}
}
