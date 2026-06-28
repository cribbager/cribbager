package bot

import (
	"math"

	"github.com/cribbager/cribbager/internal/game"
)

// Comparison is the result of A vs B over duplicate deals: the head-to-head win
// rate plus the paired margin (card luck cancelled) and its 95% CI.
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
	if pairs < 1 {
		pairs = 1
	}
	margins := make([]float64, pairs)
	var aWins, aPts, bPts int
	for p := 0; p < pairs; p++ {
		ds := seed + int64(p) // identical deal for both games in the pair

		g1, err := PlayGame(a, b, game.NewSeededDeck(ds)) // A in seat 0
		if err != nil {
			return Comparison{}, err
		}
		g2, err := PlayGame(b, a, game.NewSeededDeck(ds)) // A in seat 1
		if err != nil {
			return Comparison{}, err
		}

		if g1.Winner == game.Seat0 {
			aWins++
		}
		if g2.Winner == game.Seat1 {
			aWins++
		}
		aPts += g1.Scores[0] + g2.Scores[1]
		bPts += g1.Scores[1] + g2.Scores[0]
		margins[p] = float64((g1.Scores[0] - g1.Scores[1]) + (g2.Scores[1] - g2.Scores[0]))
	}

	mean := 0.0
	for _, m := range margins {
		mean += m
	}
	mean /= float64(pairs)

	se := 0.0
	if pairs > 1 {
		var ss float64
		for _, m := range margins {
			d := m - mean
			ss += d * d
		}
		se = math.Sqrt(ss/float64(pairs)) / math.Sqrt(float64(pairs))
	}

	games := pairs * 2
	return Comparison{
		A: a.Name(), B: b.Name(),
		Pairs: pairs, Games: games,
		AWinRate:   float64(aWins) / float64(games),
		AAvgScore:  float64(aPts) / float64(games),
		BAvgScore:  float64(bPts) / float64(games),
		Margin:     mean,
		MarginCILo: mean - 1.96*se,
		MarginCIHi: mean + 1.96*se,
	}, nil
}
