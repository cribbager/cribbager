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

	// Ceiling rung (L1c): the greedy best-case pick, off the monotonic ladder,
	// measured against the mean (L2), the floor (L1), and the champion (L4).
	var ceilVsMean, ceilVsFloor, ceilVsTop, ceilDisN int
	var ceilGapSum float64 // Σ over ceil≠mean of (E[mean pick] − E[ceil pick])

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

			// Ceiling: greedy luckiest-cut pick (score-independent).
			ceilPick := pickMaxCeil(sp)
			if ceilPick == picks[L2] {
				ceilVsMean++
			} else {
				ceilDisN++
				ceilGapSum += eHandOf(picks[L2]) - eHandOf(ceilPick) // E-value the greedy pick forfeits
			}
			if ceilPick == picks[L1] {
				ceilVsFloor++
			}
			if ceilPick == picks[L4] {
				ceilVsTop++
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

	ceilGapDis := 0.0
	if ceilDisN > 0 {
		ceilGapDis = ceilGapSum / float64(ceilDisN)
	}
	fmt.Fprintf(&b, "\nceiling rung (L1c = argmax luckiest cut over 46 starters):\n")
	fmt.Fprintf(&b, "  ceiling vs L2-mean : agree %6.2f%%   mean E[hand] the greedy pick throws away on disagreements %+.4f\n",
		100*float64(ceilVsMean)/float64(decisions), ceilGapDis)
	fmt.Fprintf(&b, "  ceiling vs L1-floor: agree %6.2f%%\n", 100*float64(ceilVsFloor)/float64(decisions))
	fmt.Fprintf(&b, "  ceiling vs L4-champ: agree %6.2f%%\n", 100*float64(ceilVsTop)/float64(decisions))
	t.Log(b.String())

	t.Log(ladderEndgameSlice(t, full, n, seed))
}

// ladderEndgameSlice is the Experiment 1 endgame block: it restricts to
// positions where the decider is clearly BEHIND and in reach (my∈[90,112],
// opp∈[my+8,120], both dealer roles) and reports how often the champion's pick
// (L4) matches the CEILING (upside) pick versus the MEAN (L2) pick — the
// hypothesis being that the champion leans toward upside when trailing. The
// discriminating rows (ceiling≠mean) are where the two hypotheses actually
// separate. Deterministic: its own seed stream, capped hand count.
func ladderEndgameSlice(t *testing.T, full []cribbage.Card, n int, seed int64) string {
	t.Helper()
	egN := n
	if egN > 50000 { // a "slice" — bound the cost of the win-walk here
		egN = 50000
	}
	egRng := rand.New(rand.NewSource(seed + 777))
	rungs := Ladder(seed + 778)

	var decisions, l4Ceil, l4Mean, ceilEqMean int
	var discrim, dl4Ceil, dl4Mean int
	for i := 0; i < egN; i++ {
		deck := append([]cribbage.Card(nil), full...)
		h := deal6(deck, egRng)
		for _, dealer := range []bool{false, true} {
			my := 90 + egRng.Intn(23)            // [90,112]
			opp := my + 8 + egRng.Intn(121-my-8) // [my+8, 120]
			if !eval.InReach(my, opp, dealer) {
				continue
			}
			sp := Splits(h, dealer)
			l4 := rungs[L4].Discard(h, dealer, my, opp)
			cp := pickMaxCeil(sp)
			mp := pickMaxEHand(sp)
			decisions++
			if l4 == cp {
				l4Ceil++
			}
			if l4 == mp {
				l4Mean++
			}
			if cp == mp {
				ceilEqMean++
				continue
			}
			discrim++
			if l4 == cp {
				dl4Ceil++
			}
			if l4 == mp {
				dl4Mean++
			}
		}
	}

	pct := func(num, den int) float64 {
		if den == 0 {
			return 0
		}
		return 100 * float64(num) / float64(den)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== Experiment 1b: ENDGAME slice — decider clearly BEHIND & in reach ===\n")
	fmt.Fprintf(&b, "positions: my∈[90,112], opp∈[my+8,120], both dealer roles   hands=%d  decisions=%d\n", egN, decisions)
	fmt.Fprintf(&b, "  champion (L4) matches ceiling (upside) pick : %6.2f%%\n", pct(l4Ceil, decisions))
	fmt.Fprintf(&b, "  champion (L4) matches mean (L2) pick        : %6.2f%%\n", pct(l4Mean, decisions))
	fmt.Fprintf(&b, "  (ceiling and mean already agree on            %6.2f%% of these decisions)\n", pct(ceilEqMean, decisions))
	fmt.Fprintf(&b, "  among the %d discriminating decisions (ceiling≠mean):\n", discrim)
	fmt.Fprintf(&b, "    L4 == ceiling (upside) : %6.2f%%\n", pct(dl4Ceil, discrim))
	fmt.Fprintf(&b, "    L4 == mean             : %6.2f%%\n", pct(dl4Mean, discrim))
	return b.String()
}

// upsideThresholds are the P(hand >= T) cut-points the desperation experiment
// measures the champion against: a good hand, a top-decile hand, a big hand.
var upsideThresholds = []int{8, 12, 16}

// sliceResult accumulates, over one position slice, how often the champion's
// discard matches the mean pick, the ceiling pick, and each threshold-upside
// pick — both overall and (per threshold) restricted to decisions where that
// upside pick actually DIFFERS from the mean (the discriminating sample).
type sliceResult struct {
	name            string
	decisions       int
	matchMean       int
	matchCeil       int
	matchUp         map[int]int // T -> L4 == upside>=T (overall)
	upDiffMean      map[int]int // T -> upside>=T pick != mean pick (discriminating count)
	matchUpWhenDiff map[int]int // T -> among discriminating, L4 == upside>=T
}

func newSliceResult(name string) sliceResult {
	return sliceResult{name: name, matchUp: map[int]int{}, upDiffMean: map[int]int{}, matchUpWhenDiff: map[int]int{}}
}

// runSlice samples N hands (both dealer roles), draws a position from sample,
// and tallies champion agreement with mean / ceiling / each upside threshold.
// Only in-reach positions count (out of reach the champion IS point-EV, so its
// win-awareness — the thing we are testing — is dormant).
func runSlice(name string, full []cribbage.Card, n int, seed int64, sample func(rng *rand.Rand) (my, opp int)) sliceResult {
	res := newSliceResult(name)
	rng := rand.New(rand.NewSource(seed))
	rungs := Ladder(seed + 1)
	for i := 0; i < n; i++ {
		deck := append([]cribbage.Card(nil), full...)
		h := deal6(deck, rng)
		for _, dealer := range []bool{false, true} {
			my, opp := sample(rng)
			if !eval.InReach(my, opp, dealer) {
				continue
			}
			sp := Splits(h, dealer)
			l4 := rungs[L4].Discard(h, dealer, my, opp)
			mean := pickMaxEHand(sp)
			ceil := pickMaxCeil(sp)
			res.decisions++
			if l4 == mean {
				res.matchMean++
			}
			if l4 == ceil {
				res.matchCeil++
			}
			for _, T := range upsideThresholds {
				up := pickMaxTail(sp, h[:], T)
				if l4 == up {
					res.matchUp[T]++
				}
				if up != mean {
					res.upDiffMean[T]++
					if l4 == up {
						res.matchUpWhenDiff[T]++
					}
				}
			}
		}
	}
	return res
}

// TestLadderDesperate is Experiment 1c: does the champion shift toward the
// threshold-upside pick — P(hand >= T), the correct formalization of "swing for
// a big hand" — when it is genuinely desperate (behind, opponent nearly home),
// versus a neutral even mid-game position? A rise in agreement with the high-T
// upside pick from NEUTRAL to DESPERATE would confirm the champion swings when it
// must; a flat/falling line suggests it does not (or that upside does not help
// even there). Opt-in and deterministic.
func TestLadderDesperate(t *testing.T) {
	if !ladderEnabled("desperate") {
		t.Skip("set LADDER=desperate (or LADDER=all) to run Experiment 1c")
	}
	n := envInt("N", 50000)
	seed := envInt64("SEED", 5150)
	full := cribbage.Deck()

	// DESPERATE: decider behind, opponent almost home — only a big deal this hand
	// keeps them alive. NEUTRAL: even, mid-board, no urgency.
	desperate := runSlice("DESPERATE (behind ~90-110, opp ~116-120)", full, n, seed,
		func(rng *rand.Rand) (int, int) {
			my := 90 + rng.Intn(21)  // [90,110]
			opp := 116 + rng.Intn(5) // [116,120]
			return my, opp
		})
	neutral := runSlice("NEUTRAL (even, mid-board 40-80)", full, n, seed+333,
		func(rng *rand.Rand) (int, int) {
			my := 40 + rng.Intn(41) // [40,80]
			opp := my - 4 + rng.Intn(9)
			if opp < 0 {
				opp = 0
			}
			return my, opp
		})

	pct := func(num, den int) float64 {
		if den == 0 {
			return 0
		}
		return 100 * float64(num) / float64(den)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== Experiment 1c: threshold-upside — desperate vs neutral ===\n")
	fmt.Fprintf(&b, "hands=%d each × 2 roles; only in-reach positions counted.\n", n)
	fmt.Fprintf(&b, "  NEUTRAL decisions=%d   DESPERATE decisions=%d\n\n", neutral.decisions, desperate.decisions)

	fmt.Fprintf(&b, "champion (L4) pick matches …           %10s  %10s\n", "NEUTRAL", "DESPERATE")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 60))
	fmt.Fprintf(&b, "  mean (L2)                            %9.2f%%  %9.2f%%\n",
		pct(neutral.matchMean, neutral.decisions), pct(desperate.matchMean, desperate.decisions))
	fmt.Fprintf(&b, "  ceiling (luckiest cut)               %9.2f%%  %9.2f%%\n",
		pct(neutral.matchCeil, neutral.decisions), pct(desperate.matchCeil, desperate.decisions))
	for _, T := range upsideThresholds {
		fmt.Fprintf(&b, "  upside>=%-2d  P(hand>=%d)               %9.2f%%  %9.2f%%\n", T, T,
			pct(neutral.matchUp[T], neutral.decisions), pct(desperate.matchUp[T], desperate.decisions))
	}

	fmt.Fprintf(&b, "\nTHE SHIFT — on discriminating decisions (upside>=T pick ≠ mean pick),\n")
	fmt.Fprintf(&b, "how often the champion sides with the upside pick over the mean:\n")
	fmt.Fprintf(&b, "%-22s %20s  %20s\n", "", "NEUTRAL", "DESPERATE")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 66))
	for _, T := range upsideThresholds {
		fmt.Fprintf(&b, "  upside>=%-2d            %8.2f%% (n=%-6d)  %8.2f%% (n=%-6d)\n", T,
			pct(neutral.matchUpWhenDiff[T], neutral.upDiffMean[T]), neutral.upDiffMean[T],
			pct(desperate.matchUpWhenDiff[T], desperate.upDiffMean[T]), desperate.upDiffMean[T])
	}
	t.Log(b.String())
}

// TestLadderOppCrib is Experiment 3: does modeling the opponent's discards
// (weighting their crib cards by the role-aware keep model) change the discard,
// and does it WIN over the uniform-crib evaluator (L3)? Two parts: an agreement
// / crib-bias tally split by whose crib it is, and a discard-isolated
// head-to-head. Opt-in (LADDER=oppcrib).
func TestLadderOppCrib(t *testing.T) {
	if !ladderEnabled("oppcrib") {
		t.Skip("set LADDER=oppcrib (or LADDER=all) to run Experiment 3")
	}
	n := envInt("N", 100000)
	pairsN := envInt("PAIRS", 5000)
	seed := envInt64("SEED", 1)
	full := cribbage.Deck()

	// --- Part A: agreement & crib bias, split by whose crib. ---
	rng := rand.New(rand.NewSource(seed))
	// per role: [0]=their crib (pone), [1]=our crib (dealer)
	var decisions, agree [2]int
	var cribBiasSum, scoreGapSum [2]float64 // Σ(oppCrib−uniformCrib) on chosen; Σ point-EV Score conceded on disagreement
	var disN [2]int
	for i := 0; i < n; i++ {
		deck := append([]cribbage.Card(nil), full...)
		h := deal6(deck, rng)
		for _, dealer := range []bool{false, true} {
			r := 0
			if dealer {
				r = 1
			}
			sp := Splits(h, dealer)
			uni := pickMaxScore(sp)                       // L3 uniform crib
			opp := OppCribRung().Discard(h, dealer, 0, 0) // opponent-modeled crib
			decisions[r]++
			// crib bias on the UNIFORM pick: how much lower/higher the modeled crib
			// values that same discard vs the uniform table (unsigned crib values).
			us := findSplit(sp, uni)
			cribBiasSum[r] += eval.OppCribEV(us.Discard[0], us.Discard[1], dealer) - eval.CribEV(us.Discard[0], us.Discard[1])
			if uni == opp {
				agree[r]++
			} else {
				disN[r]++
				// point-EV Score (uniform objective) the opp-crib pick concedes.
				scoreGapSum[r] += findSplit(sp, uni).Score - findSplit(sp, opp).Score
			}
		}
	}

	pct := func(a, b int) float64 {
		if b == 0 {
			return 0
		}
		return 100 * float64(a) / float64(b)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== Experiment 3: opponent-modeled crib vs uniform crib (L3) ===\n")
	fmt.Fprintf(&b, "hands=%d × 2 roles  seed=%d\n\n", n, seed)
	fmt.Fprintf(&b, "%-14s  %10s  %10s  %14s  %16s\n", "whose crib", "decisions", "agree%", "crib bias (pts)", "Score conceded/dis")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 70))
	labels := [2]string{"their crib", "our crib"}
	for r := 0; r < 2; r++ {
		bias := cribBiasSum[r] / float64(decisions[r])
		gap := 0.0
		if disN[r] > 0 {
			gap = scoreGapSum[r] / float64(disN[r])
		}
		fmt.Fprintf(&b, "%-14s  %10d  %9.2f%%  %+14.4f  %+16.4f\n", labels[r], decisions[r], pct(agree[r], decisions[r]), bias, gap)
	}
	fmt.Fprintf(&b, "\n(crib bias = modeled crib EV − uniform crib EV on the discard L3 chose;\n negative means the uniform table is OPTIMISTIC about that crib.)\n")

	// --- Part B: discard-isolated head-to-head — does opp-crib beat uniform? ---
	uniBot := Bot(Ladder(seed + 1)[L3])
	oppBot := Bot(OppCribRung())
	c, err := bot.Compare(oppBot, uniBot, pairsN, seed+2)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "\nhead-to-head (champion pegging both sides, %d duplicate deal-pairs):\n", pairsN)
	fmt.Fprintf(&b, "  L3-oppcrib vs L3-uniform : %+0.3f pts/pair  95%% CI [%+0.3f, %+0.3f]   windiff %+0.4f\n",
		c.Margin, c.MarginCILo, c.MarginCIHi, c.WinDiff)
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
