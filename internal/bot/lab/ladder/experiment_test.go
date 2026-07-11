package ladder

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
)

// The ladder experiments are OPT-IN, like the lab's promotion gate — a plain
// `go test ./...` skips them, since Experiment 2 plays millions of games. Run
// them by setting LADDER:
//
//	LADDER=agree    N=200000 SEED=1  go test ./internal/bot/lab/ladder -run Agreement -v
//	LADDER=strength PAIRS=5000 SEED=90000 go test ./internal/bot/lab/ladder -run Strength -v -timeout 60m
//	LADDER=all      go test ./internal/bot/lab/ladder -run 'Agreement|Strength' -v -timeout 60m
//
// N / PAIRS / SEED override the defaults below.

func ladderEnabled(which string) bool {
	v := strings.ToLower(os.Getenv("LADDER"))
	return v == "all" || v == which
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		var n int64
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}

// deal6 draws a random six-card hand from a full deck using rng (Fisher-Yates
// on the first six positions). Deterministic for a given rng stream.
func deal6(deck []cribbage.Card, rng *rand.Rand) [6]cribbage.Card {
	for i := 0; i < 6; i++ {
		j := i + rng.Intn(len(deck)-i)
		deck[i], deck[j] = deck[j], deck[i]
	}
	var h [6]cribbage.Card
	copy(h[:], deck[:6])
	return h
}

// TestLadderAgreement is Experiment 1: over N seeded random six-card hands, at a
// seeded random game position each, how often does each rung agree with the next
// (and with the champion), and how much of its objective does the lower rung
// give up when they differ? Deterministic and reproducible.
func TestLadderAgreement(t *testing.T) {
	if !ladderEnabled("agree") {
		t.Skip("set LADDER=agree (or LADDER=all) to run Experiment 1")
	}
	n := envInt("N", 200000)
	seed := envInt64("SEED", 1)
	rng := rand.New(rand.NewSource(seed))
	full := cribbage.Deck()

	// consecutive rung pairs (lower, higher) and the objective each disagreement
	// is charged against. The gap is HIGHER's objective at HIGHER's pick minus at
	// LOWER's pick — how much of the higher rung's own yardstick the lower pick
	// forfeits. Non-negative by construction for L0..L3 (argmaxes of that same
	// objective); for L3→L4 we charge the point-EV Score the champion concedes to
	// win probability (so it can be negative — that is the point).
	type pairStat struct {
		lo, hi int
		metric string
		agree  int
		disN   int
		gapSum float64 // sum over disagreements of hi-objective gap (hi pick − lo pick)
	}
	pairs := []pairStat{
		{lo: L0, hi: L1, metric: "intrinsic show"},
		{lo: L1, hi: L2, metric: "E[hand]"},
		{lo: L2, hi: L3, metric: "Score (E[hand]+crib)"},
		{lo: L3, hi: L4, metric: "Score conceded"},
	}
	// agreement of each rung with the top (L4).
	agreeTop := make([]int, 5)
	inReach := 0

	// Reusable ladder (L0's RNG is separate from the dealing RNG).
	rungs := Ladder(seed + 1)

	// Sweep both dealer roles inside the hand loop, so a single N counts
	// N hands × 2 roles of decisions (covering the crib sign and the win table).
	for i := 0; i < n; i++ {
		deck := append([]cribbage.Card(nil), full...)
		h := deal6(deck, rng)
		for _, dealer := range []bool{false, true} {
			my := rng.Intn(121)
			opp := rng.Intn(121)
			if eval.InReach(my, opp, dealer) {
				inReach++
			}

			sp := Splits(h, dealer)
			// picks[L0..L4]
			var picks [5][2]cribbage.Card
			picks[L0] = rungs[L0].Discard(h, dealer, my, opp)
			picks[L1] = pickMaxShow(sp)
			picks[L2] = pickMaxEHand(sp)
			picks[L3] = pickMaxScore(sp)
			picks[L4] = rungs[L4].Discard(h, dealer, my, opp)

			// objective values indexed by pick, per metric, read from sp.
			showOf := func(p [2]cribbage.Card) int { return findSplit(sp, p).Show }
			eHandOf := func(p [2]cribbage.Card) float64 { return findSplit(sp, p).EHand }
			scoreOf := func(p [2]cribbage.Card) float64 { return findSplit(sp, p).Score }

			for k := range pairs {
				ps := &pairs[k]
				lo, hi := picks[ps.lo], picks[ps.hi]
				if lo == hi {
					ps.agree++
					continue
				}
				ps.disN++
				switch ps.metric {
				case "intrinsic show":
					ps.gapSum += float64(showOf(hi) - showOf(lo))
				case "E[hand]":
					ps.gapSum += eHandOf(hi) - eHandOf(lo)
				default: // Score-based
					ps.gapSum += scoreOf(hi) - scoreOf(lo)
				}
			}
			for r := L0; r <= L3; r++ {
				if picks[r] == picks[L4] {
					agreeTop[r]++
				}
			}
		}
	}

	decisions := n * 2
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== Experiment 1: agreement & objective-gap ===\n")
	fmt.Fprintf(&b, "hands=%d  decisions=%d (both dealer roles)  seed=%d  in-reach positions=%.1f%%\n\n",
		n, decisions, seed, 100*float64(inReach)/float64(decisions))

	fmt.Fprintf(&b, "%-14s  %-24s  %8s  %14s  %14s\n",
		"rung pair", "gap metric", "agree%", "mean gap/dis", "mean gap/all")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 82))
	for _, ps := range pairs {
		agreePct := 100 * float64(ps.agree) / float64(decisions)
		gapDis, gapAll := 0.0, ps.gapSum/float64(decisions)
		if ps.disN > 0 {
			gapDis = ps.gapSum / float64(ps.disN)
		}
		fmt.Fprintf(&b, "%-3s→%-10s  %-24s  %7.2f%%  %+14.4f  %+14.4f\n",
			rungs[ps.lo].Name[:2], rungs[ps.hi].Name, ps.metric, agreePct, gapDis, gapAll)
	}

	fmt.Fprintf(&b, "\nagreement with L4 champion (top):\n")
	for r := L0; r <= L3; r++ {
		fmt.Fprintf(&b, "  %-12s %6.2f%%\n", rungs[r].Name, 100*float64(agreeTop[r])/float64(decisions))
	}
	t.Log(b.String())
}

// findSplit returns the split whose discard is the given (unordered) pair.
func findSplit(sp []Split, d [2]cribbage.Card) Split {
	for _, s := range sp {
		if s.Discard == d || (s.Discard[0] == d[1] && s.Discard[1] == d[0]) {
			return s
		}
	}
	// Every legal discard is one of the 15 splits, so this is unreachable.
	panic(fmt.Sprintf("ladder: discard %v not among splits", d))
}

// TestLadderStrength is Experiment 2: each rung's discard paired with champion
// pegging, played over duplicate deals. Reports each rung vs the champion (L4)
// and vs the rung below it — "how many points each added concept buys" — with
// 95% CIs from bot.Compare.
func TestLadderStrength(t *testing.T) {
	if !ladderEnabled("strength") {
		t.Skip("set LADDER=strength (or LADDER=all) to run Experiment 2")
	}
	pairs := envInt("PAIRS", 5000)
	seed := envInt64("SEED", 90000)

	bots := Bots(seed) // one bot per rung (shared L0 RNG seed)
	top := bots[L4]    // L4 = champion discard + champion pegging
	name := func(i int) string { return bots[i].Name() }

	run := func(a, b bot.Bot) bot.Comparison {
		// Fresh bots each comparison so L0's RNG restarts — reproducible.
		c, err := bot.Compare(a, b, pairs, seed)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	var out strings.Builder
	fmt.Fprintf(&out, "\n=== Experiment 2: discard-isolated strength (champion pegging both sides) ===\n")
	fmt.Fprintf(&out, "deal-pairs=%d (%d games each)  seed=%d\n\n", pairs, pairs*2, seed)

	fmt.Fprintf(&out, "%-28s  %14s  %-22s  %14s\n",
		"matchup", "margin/pair", "95% CI (points)", "windiff/pair")
	fmt.Fprintf(&out, "%s\n", strings.Repeat("-", 86))

	// Each rung vs the champion (L4).
	fmt.Fprintf(&out, "-- vs champion (L4) --\n")
	for r := L0; r <= L3; r++ {
		freshLadder := Bots(seed) // independent bot instances per matchup
		c := run(freshLadder[r], top)
		fmt.Fprintf(&out, "%-28s  %+13.3f  [%+7.3f, %+7.3f]  %+13.4f\n",
			name(r)+" vs "+name(L4), c.Margin, c.MarginCILo, c.MarginCIHi, c.WinDiff)
	}

	// Each rung vs the rung below — the marginal value of each added concept.
	fmt.Fprintf(&out, "-- vs the rung below (marginal value of each concept) --\n")
	for r := L1; r <= L4; r++ {
		fresh := Bots(seed)
		c := run(fresh[r], fresh[r-1])
		fmt.Fprintf(&out, "%-28s  %+13.3f  [%+7.3f, %+7.3f]  %+13.4f\n",
			name(r)+" vs "+name(r-1), c.Margin, c.MarginCILo, c.MarginCIHi, c.WinDiff)
	}
	t.Log(out.String())
}
