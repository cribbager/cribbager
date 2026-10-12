package vectors

import (
	"fmt"
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// PegStep is the score of one play within a count sequence, as six integers:
// [count, total, fifteen, thirtyOne, pair, run]. count is the running count
// after the card; pair is 2, 6 or 12; run is the run length (which is also its
// points) or 0; total is the sum of the last four.
type PegStep [6]int

// PegVector is one count sequence (cards played in order, alternating players,
// no resets inside it) and the points each play scores. Go/last-card points
// belong to the game flow, not to a single play, so they are not here.
type PegVector struct {
	Name  string    `json:"name,omitempty"`
	Plays []string  `json:"plays"`
	Steps []PegStep `json:"steps"`
}

func pegVector(name string, seq []cribbage.Card) PegVector {
	v := PegVector{Name: name, Plays: encAll(seq)}
	for i, c := range seq {
		res, err := pegging.Score(seq[:i], c)
		if err != nil {
			panic(fmt.Sprintf("vectors: illegal pegging sequence %v: %v", seq, err))
		}
		v.Steps = append(v.Steps, PegStep{
			res.Count, res.Total, res.Fifteen.Points, res.ThirtyOne.Points, res.Pair.Points, res.Run.Points,
		})
	}
	return v
}

type canonPeg struct {
	name string
	seq  []string
	// last is the expected total of the final play, asserted against Go (-1 = none).
	last int
}

var canonicalPegs = []canonPeg{
	{"fifteen with two cards", []string{"7C", "8D"}, 2},
	{"fifteen with a face card and a five", []string{"KC", "5D"}, 2},
	{"fifteen from three cards", []string{"AC", "4D", "10H"}, 2},
	{"thirty-one", []string{"KC", "QD", "9H", "2S"}, 2},
	{"thirty-one made with three tens and an ace", []string{"10C", "10D", "10H", "AS"}, 2},
	{"pair", []string{"4C", "4D"}, 2},
	{"pair royal", []string{"4C", "4D", "4H"}, 6},
	{"double pair royal", []string{"4C", "4D", "4H", "4S"}, 12},
	{"faces pair by rank, not by value (J then Q is no pair)", []string{"JC", "QD"}, 0},
	{"faces pair by rank: two queens", []string{"QC", "QD"}, 2},
	{"pair must be adjacent", []string{"4C", "5D", "4H"}, 0},
	{"pair broken by another card: 4-4-5-4", []string{"4C", "4D", "5H", "4S"}, 0},
	{"pair royal then different card then pair", []string{"6C", "6D", "6H", "2S", "2C"}, 2},
	{"run of three, in order", []string{"AC", "2D", "3H"}, 3},
	{"run of three, out of order", []string{"3C", "AD", "2H"}, 3},
	{"run of four, shuffled", []string{"2C", "5D", "3H", "4S"}, 4},
	{"run takes longest tail: 9-3-4-5", []string{"9C", "3D", "4H", "5S"}, 3},
	{"run broken by duplicate: 3-4-4-5 scores nothing on the 5", []string{"3C", "4D", "4H", "5S"}, 0},
	{"no wraparound: Q-K-A", []string{"QC", "KD", "AH"}, 0},
	{"no wraparound: K-A-2", []string{"KC", "AD", "2H"}, 0},
	{"run of seven with fifteen along the way", []string{"AC", "2D", "3H", "4S", "5C", "6D", "7H"}, 7},
	{"fifteen and run together: 4-5-6", []string{"4C", "5D", "6H"}, 5},
	{"fifteen and run together, out of order: 6-4-5", []string{"6C", "4D", "5H"}, 5},
	{"fifteen and pair royal: 5-5-5", []string{"5C", "5D", "5H"}, 8},
	{"pair after a fifteen has been passed: 7-8-8", []string{"7C", "8D", "8H"}, 2},
	{"trips at the end of a count: A-10-10-10", []string{"AC", "10D", "10H", "10S"}, 8},
	{"run of six that also makes 31: 10-A-2-3-4-5-6", []string{"10C", "AD", "2H", "3S", "4C", "5D", "6H"}, -1},
	{"fifteen early, thirty-one at the end: K-5-Q-A-5", []string{"KC", "5D", "QH", "AS", "5C"}, -1},
	{"run of four then extending to five", []string{"3C", "4D", "5H", "6S", "7C"}, -1},
	{"extending run: 2-3-4-5 then a 6 after a 2", []string{"2C", "3D", "4H", "5S", "6C"}, -1},
	{"cards that bridge a run in the middle: 5-7-6", []string{"5C", "7D", "6H"}, 3},
	{"six-card tail with only last four a run: K-3-4-5-6", []string{"KC", "3D", "4H", "5S", "6C"}, 4},
}

func pegVectors(seed int64) []PegVector {
	var out []PegVector
	for _, c := range canonicalPegs {
		if len(c.seq) == 0 {
			continue
		}
		v := pegVector(c.name, cards(c.seq...))
		if c.last >= 0 && v.Steps[len(v.Steps)-1][1] != c.last {
			panic(fmt.Sprintf("vectors: canonical pegging %q: last play scores %d in Go, expected %d",
				c.name, v.Steps[len(v.Steps)-1][1], c.last))
		}
		out = append(out, v)
	}

	rng := rand.New(rand.NewSource(seed))
	deck := cribbage.Deck()
	for i := 0; i < 450; i++ {
		// Every third sequence draws from a narrow rank window so runs and
		// repeated ranks are common; the rest are uniform.
		pool := deck
		if i%3 != 0 {
			lo := 1 + rng.Intn(9)
			width := 4 + rng.Intn(3)
			pool = nil
			for _, c := range deck {
				if int(c.Rank) >= lo && int(c.Rank) < lo+width {
					pool = append(pool, c)
				}
			}
		}
		avail := append([]cribbage.Card(nil), pool...)
		rng.Shuffle(len(avail), func(a, b int) { avail[a], avail[b] = avail[b], avail[a] })
		var seq []cribbage.Card
		count := 0
		for len(seq) < 10 {
			picked := -1
			for k, c := range avail {
				if count+c.Rank.PipValue() <= 31 {
					picked = k
					break
				}
			}
			if picked < 0 {
				break
			}
			c := avail[picked]
			avail = append(avail[:picked], avail[picked+1:]...)
			seq = append(seq, c)
			count += c.Rank.PipValue()
			if count == 31 {
				break
			}
		}
		if len(seq) >= 2 {
			out = append(out, pegVector("", seq))
		}
	}
	return out
}
