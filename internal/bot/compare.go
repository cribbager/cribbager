package bot

import (
	"math"

	"github.com/cribbager/cribbager/internal/game"
)

// Comparison is the result of A vs B over duplicate deals: the head-to-head win
// rate plus two paired instruments (card luck cancelled), each with a 95% CI.
//
// Margin is in points and is the right gate for point-EV changes. WinDiff is in
// wins and is the right gate for score-aware changes: a bot that trades points
// for win probability (defensive when ahead, desperate when behind) can show a
// flat or negative Margin while genuinely winning more — the points instrument
// is structurally blind to it.
type Comparison struct {
	A, B       string
	Pairs      int     // deal-pairs played (each is two games, seats swapped)
	Games      int     // 2 * Pairs
	AWinRate   float64 // fraction of games A won
	AAvgScore  float64
	BAvgScore  float64
	Margin     float64 // mean (A−B) per deal-pair, card luck cancelled
	MarginCILo float64 // 95% CI on Margin
	MarginCIHi float64

	PairsAWinBoth int     // discordant pairs: A won both games
	PairsSplit    int     // concordant pairs: one win each
	PairsBWinBoth int     // discordant pairs: B won both games
	WinDiff       float64 // mean per-pair win difference, (A wins − 1) ∈ {−1, 0, +1}
	WinDiffCILo   float64 // 95% CI on WinDiff
	WinDiffCIHi   float64
}

// Compare plays a vs b over `pairs` DUPLICATE deals — each dealt twice with the
// seats swapped (same deck seed both times, so the luck of the cards cancels) —
// and reports the paired margin with a 95% CI. This is the trustworthy instrument
// for "is the challenger better than the champion?": judge by the margin's CI
// clearing zero, not the raw win rate (the paired margin is far tighter than the
// same number of independent games).
//
// a and b are reused across all games, so a stateful bot's RNG keeps advancing;
// deterministic bots give the same result for a given seed.
func Compare(a, b Bot, pairs int, seed int64) (Comparison, error) {
	return compare(a, b, pairs, seed, func(x, y Bot, ds int64) (Result, error) {
		return PlayGame(x, y, game.NewSeededDeck(ds))
	})
}

// CompareFrom is Compare with every game starting from the preset position — the
// high-power instrument for score-aware changes, which are nearly invisible in
// full games from 0–0 but concentrated at endgame scores. The position attaches
// to the SEAT, not the bot: both games of a pair use the same Start (seat 0
// always holds Scores[0], the same seat deals); only the bots swap, so the
// luck-cancellation property of duplicate deals is preserved.
func CompareFrom(a, b Bot, pairs int, seed int64, start game.Start) (Comparison, error) {
	return compare(a, b, pairs, seed, func(x, y Bot, ds int64) (Result, error) {
		return PlayGameFrom(x, y, game.NewSeededDeck(ds), start)
	})
}

func compare(a, b Bot, pairs int, seed int64, play func(x, y Bot, ds int64) (Result, error)) (Comparison, error) {
	if pairs < 1 {
		pairs = 1
	}
	margins := make([]float64, pairs)
	winDiffs := make([]float64, pairs)
	var aWins, aPts, bPts int
	var aBoth, split, bBoth int
	for p := 0; p < pairs; p++ {
		ds := seed + int64(p) // identical deal for both games in the pair

		g1, err := play(a, b, ds) // A in seat 0
		if err != nil {
			return Comparison{}, err
		}
		g2, err := play(b, a, ds) // A in seat 1
		if err != nil {
			return Comparison{}, err
		}

		pairWins := 0
		if g1.Winner == game.Seat0 {
			pairWins++
		}
		if g2.Winner == game.Seat1 {
			pairWins++
		}
		aWins += pairWins
		switch pairWins {
		case 2:
			aBoth++
		case 1:
			split++
		case 0:
			bBoth++
		}
		winDiffs[p] = float64(pairWins - 1)
		aPts += g1.Scores[0] + g2.Scores[1]
		bPts += g1.Scores[1] + g2.Scores[0]
		margins[p] = float64((g1.Scores[0] - g1.Scores[1]) + (g2.Scores[1] - g2.Scores[0]))
	}

	margin, marginSE := meanSE(margins)
	winDiff, winDiffSE := meanSE(winDiffs)

	games := pairs * 2
	return Comparison{
		A: a.Name(), B: b.Name(),
		Pairs: pairs, Games: games,
		AWinRate:   float64(aWins) / float64(games),
		AAvgScore:  float64(aPts) / float64(games),
		BAvgScore:  float64(bPts) / float64(games),
		Margin:     margin,
		MarginCILo: margin - 1.96*marginSE,
		MarginCIHi: margin + 1.96*marginSE,

		PairsAWinBoth: aBoth,
		PairsSplit:    split,
		PairsBWinBoth: bBoth,
		WinDiff:       winDiff,
		WinDiffCILo:   winDiff - 1.96*winDiffSE,
		WinDiffCIHi:   winDiff + 1.96*winDiffSE,
	}, nil
}

// meanSE is the sample mean and its standard error (0 for a single sample).
func meanSE(xs []float64) (mean, se float64) {
	n := float64(len(xs))
	for _, x := range xs {
		mean += x
	}
	mean /= n
	if len(xs) > 1 {
		var ss float64
		for _, x := range xs {
			d := x - mean
			ss += d * d
		}
		se = math.Sqrt(ss/n) / math.Sqrt(n)
	}
	return mean, se
}
