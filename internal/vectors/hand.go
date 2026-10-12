package vectors

import (
	"fmt"
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// HandVector is one hand-scoring case. Points are split by category; Pairs
// includes the pairs inside a double/triple run (a run of length L with M
// distinct combinations contributes L*M to Runs and the pairs it contains to
// Pairs), so Total = Fifteens + Pairs + Runs + Flush + Nobs always holds.
type HandVector struct {
	Name      string   `json:"name,omitempty"`
	Hand      []string `json:"hand"`
	Starter   string   `json:"starter"`
	IsCrib    bool     `json:"isCrib"`
	Total     int      `json:"total"`
	Fifteens  int      `json:"fifteens"`
	Pairs     int      `json:"pairs"`
	Runs      int      `json:"runs"`
	RunLength int      `json:"runLength"`
	RunCount  int      `json:"runCount"`
	Flush     int      `json:"flush"`
	Nobs      int      `json:"nobs"`
}

func handVector(name string, h []cribbage.Card, starter cribbage.Card, isCrib bool) HandVector {
	res, err := hand.Score(four(h), starter, isCrib)
	if err != nil {
		panic(err)
	}
	v := HandVector{Name: name, Hand: encAll(h), Starter: enc(starter), IsCrib: isCrib, Total: res.Total}
	for _, c := range res.Fifteens {
		v.Fifteens += c.Points
	}
	for _, c := range res.Pairs {
		v.Pairs += c.Points
	}
	for _, c := range res.Runs {
		v.RunLength = c.RunLength
		v.RunCount = c.Multiplicity
		v.Runs = c.RunLength * c.Multiplicity
		v.Pairs += c.Points - v.Runs // pairs bundled inside the run
	}
	v.Flush = res.Flush.Points
	v.Nobs = res.Nobs.Points
	if v.Fifteens+v.Pairs+v.Runs+v.Flush+v.Nobs != v.Total {
		panic(fmt.Sprintf("vectors: breakdown does not sum for %v + %v", h, starter))
	}
	return v
}

type canonHand struct {
	name    string
	hand    []string
	starter string
	crib    bool
	want    int // expected total, asserted against the Go engine; -1 = not asserted
}

var canonicalHands = []canonHand{
	{"perfect 29: J5-5-5 with the 5 of the jack's suit as starter", []string{"JS", "5C", "5D", "5H"}, "5S", false, 29},
	{"28: J5-5-5 with the starter 5 not matching the jack", []string{"JH", "5C", "5D", "5H"}, "5S", false, 28},
	{"28: 5-5-5-5 with a ten starter", []string{"5S", "5C", "5D", "5H"}, "10S", false, 28},
	{"29 in the crib is still 29", []string{"JS", "5C", "5D", "5H"}, "5S", true, 29},
	{"zero hand: even cards cannot make fifteen", []string{"2H", "4C", "6D", "8S"}, "10H", false, 0},
	{"zero hand in the crib", []string{"2H", "4C", "6D", "8S"}, "10H", true, 0},
	{"triple run: 3-3-3-4-5", []string{"3C", "3D", "3H", "4S"}, "5C", false, 21},
	{"double-double run: 4-4-5-5-6", []string{"4C", "4D", "5H", "5S"}, "6C", false, 24},
	{"double run of 3: 3-3-4-5 plus 9", []string{"3C", "3D", "4H", "5S"}, "9C", false, -1},
	{"double run of 4: 7-8-8-9-10", []string{"7C", "8D", "8H", "9S"}, "10C", false, -1},
	{"run of five: 3-4-5-6-7", []string{"3C", "4D", "5H", "6S"}, "7C", false, 9},
	{"run of three with a gap: 6-7-8 and 3-4 dead", []string{"3C", "4D", "6H", "7S"}, "8C", false, -1},
	{"run with the starter in the middle", []string{"9C", "JD", "QH", "2S"}, "10C", false, -1},
	{"no wraparound: Q-K-A is not a run", []string{"QC", "KD", "AS", "9H"}, "3D", false, -1},
	{"no wraparound: K-A-2 is not a run", []string{"KC", "AD", "2S", "9H"}, "4D", false, -1},
	{"low run: A-2-3", []string{"AC", "2D", "3S", "9H"}, "KD", false, 7},
	{"four-card flush in hand scores 4", []string{"2H", "4H", "6H", "8H"}, "10C", false, 4},
	{"four-card flush in the crib scores 0", []string{"2H", "4H", "6H", "8H"}, "10C", true, 0},
	{"five-card flush in hand scores 5", []string{"2H", "4H", "6H", "8H"}, "10H", false, 5},
	{"five-card flush in the crib scores 5", []string{"2H", "4H", "6H", "8H"}, "10H", true, 5},
	{"four hand cards flush but starter is off-suit, crib, with fifteens", []string{"5H", "10H", "JH", "KH"}, "5C", true, -1},
	{"three-card flush scores nothing", []string{"2H", "4H", "6H", "8C"}, "10H", false, 0},
	{"starter matches three hand cards only: no flush", []string{"2H", "4H", "6H", "8C"}, "10H", false, 0},
	{"nobs: jack in hand matching starter suit", []string{"JH", "2C", "4D", "6S"}, "8H", false, 1},
	{"no nobs: jack suit differs from starter", []string{"JH", "2C", "4D", "6S"}, "8S", false, 0},
	{"no nobs for a jack starter", []string{"2C", "4D", "6S", "8H"}, "JH", false, 0},
	{"nobs counts in the crib", []string{"JH", "2C", "4D", "6S"}, "8H", true, 1},
	{"nobs plus five-card flush", []string{"JH", "2H", "4H", "6H"}, "8H", false, 6},
	{"nobs with 5s: J-5 and starter 5 of jack's suit", []string{"JD", "5C", "2H", "9S"}, "5D", false, -1},
	{"max fifteens: 5-5-5-10-J style", []string{"5C", "5D", "5H", "10S"}, "JC", false, -1},
	{"many fifteens: 7-7-8-8-A", []string{"7C", "8D", "7H", "8S"}, "AC", false, -1},
	{"pair royal (three of a kind)", []string{"9C", "9D", "9H", "2S"}, "KC", false, -1},
	{"four of a kind", []string{"7C", "7D", "7H", "7S"}, "KC", false, -1},
	{"ace counts one for fifteens", []string{"AC", "AD", "AH", "QS"}, "KC", false, -1},
	{"face cards all count ten", []string{"JC", "QD", "KH", "5S"}, "AC", false, -1},
}

func handVectors(seed int64) []HandVector {
	var out []HandVector
	for _, c := range canonicalHands {
		v := handVector(c.name, cards(c.hand...), card(c.starter), c.crib)
		if c.want >= 0 && v.Total != c.want {
			panic(fmt.Sprintf("vectors: canonical hand %q scores %d in the Go engine, expected %d", c.name, v.Total, c.want))
		}
		out = append(out, v)
	}

	rng := rand.New(rand.NewSource(seed))
	deck := cribbage.Deck()
	draw := func(pred func(cribbage.Card) bool, n int, banned map[cribbage.Card]bool) []cribbage.Card {
		var picked []cribbage.Card
		for len(picked) < n {
			c := deck[rng.Intn(52)]
			if banned[c] || !pred(c) {
				continue
			}
			banned[c] = true
			picked = append(picked, c)
		}
		return picked
	}
	any := func(cribbage.Card) bool { return true }

	// Uniform random hands.
	for i := 0; i < 700; i++ {
		banned := map[cribbage.Card]bool{}
		cs := draw(any, 5, banned)
		out = append(out, handVector("", cs[:4], cs[4], rng.Intn(2) == 0))
	}
	// Flush-rich: four hand cards of one suit, starter on or off suit, hand and crib.
	for i := 0; i < 150; i++ {
		banned := map[cribbage.Card]bool{}
		suit := cribbage.Suit(rng.Intn(4))
		h := draw(func(c cribbage.Card) bool { return c.Suit == suit }, 4, banned)
		var s []cribbage.Card
		if rng.Intn(2) == 0 {
			s = draw(func(c cribbage.Card) bool { return c.Suit == suit }, 1, banned)
		} else {
			s = draw(func(c cribbage.Card) bool { return c.Suit != suit }, 1, banned)
		}
		out = append(out, handVector("", h, s[0], rng.Intn(2) == 0))
	}
	// Run/pair-rich: ranks confined to a narrow window so runs and doubles are common.
	for i := 0; i < 250; i++ {
		banned := map[cribbage.Card]bool{}
		lo := 1 + rng.Intn(9)
		cs := draw(func(c cribbage.Card) bool { return int(c.Rank) >= lo && int(c.Rank) < lo+5 }, 5, banned)
		out = append(out, handVector("", cs[:4], cs[4], rng.Intn(2) == 0))
	}
	// Jack-rich: exercise nobs and the jack-as-starter case.
	for i := 0; i < 100; i++ {
		banned := map[cribbage.Card]bool{}
		j := cribbage.Card{Rank: cribbage.Jack, Suit: cribbage.Suit(rng.Intn(4))}
		banned[j] = true
		rest := draw(any, 4, banned)
		cs := append([]cribbage.Card{j}, rest[:3]...)
		starter := rest[3]
		if rng.Intn(2) == 0 {
			starter = draw(func(c cribbage.Card) bool { return c.Suit == j.Suit }, 1, banned)[0]
		}
		out = append(out, handVector("", cs, starter, rng.Intn(2) == 0))
	}
	return out
}
