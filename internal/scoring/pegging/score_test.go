package pegging

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// --- Golden vectors ------------------------------------------------------------

type vector struct {
	Name   string   `json:"name"`
	Series []string `json:"series"`
	Card   string   `json:"card"`
	Count  int      `json:"count"`
	Total  int      `json:"total"`
	Points points   `json:"points"`
	RunLen int      `json:"runLength"`
}

type points struct {
	Fifteen   int `json:"fifteen"`
	ThirtyOne int `json:"thirtyOne"`
	Pair      int `json:"pair"`
	Run       int `json:"run"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	path := filepath.Join("testdata", "pegging_scores.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading vectors: %v", err)
	}
	var vs []vector
	if err := json.Unmarshal(data, &vs); err != nil {
		t.Fatalf("parsing vectors: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("no vectors loaded")
	}
	return vs
}

func (v vector) parse(t *testing.T) ([]cribbage.Card, cribbage.Card) {
	t.Helper()
	series := make([]cribbage.Card, len(v.Series))
	for i, s := range v.Series {
		c, err := cribbage.ParseCard(s)
		if err != nil {
			t.Fatalf("%s: bad card %q: %v", v.Name, s, err)
		}
		series[i] = c
	}
	card, err := cribbage.ParseCard(v.Card)
	if err != nil {
		t.Fatalf("%s: bad card %q: %v", v.Name, v.Card, err)
	}
	return series, card
}

func TestGoldenVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			series, card := v.parse(t)

			res, err := Score(series, card)
			if err != nil {
				t.Fatalf("Score error: %v", err)
			}
			if res.Total != v.Total {
				t.Errorf("total = %d, want %d", res.Total, v.Total)
			}
			if res.Count != v.Count {
				t.Errorf("count = %d, want %d", res.Count, v.Count)
			}
			got := points{res.Fifteen.Points, res.ThirtyOne.Points, res.Pair.Points, res.Run.Points}
			if got != v.Points {
				t.Errorf("points = %+v, want %+v", got, v.Points)
			}
			if v.RunLen > 0 && res.Run.RunLength != v.RunLen {
				t.Errorf("run length = %d, want %d", res.Run.RunLength, v.RunLen)
			}

			if tot, _ := Total(series, card); tot != v.Total {
				t.Errorf("Total = %d, want %d", tot, v.Total)
			}
			if ref := referenceTotal(series, card); ref != v.Total {
				t.Errorf("referenceTotal = %d, want %d", ref, v.Total)
			}
		})
	}
}

// --- Invalid input -------------------------------------------------------------

func TestRejectsIllegalPlays(t *testing.T) {
	c := func(s string) cribbage.Card {
		card, err := cribbage.ParseCard(s)
		if err != nil {
			t.Fatal(err)
		}
		return card
	}

	// Past 31: 10 + 10 + 10 = 30, then a 2 would make 32.
	over := []cribbage.Card{c("TH"), c("TC"), c("TD")}
	if _, err := Score(over, c("2S")); !errors.Is(err, ErrCountExceeds31) {
		t.Errorf("over-31 Score: err = %v, want ErrCountExceeds31", err)
	}
	if _, err := Total(over, c("2S")); !errors.Is(err, ErrCountExceeds31) {
		t.Errorf("over-31 Total: err = %v, want ErrCountExceeds31", err)
	}

	// Duplicate physical card.
	dupSeries := []cribbage.Card{c("5H"), c("6C")}
	if _, err := Score(dupSeries, c("5H")); !errors.Is(err, ErrDuplicateCard) {
		t.Errorf("duplicate Score: err = %v, want ErrDuplicateCard", err)
	}
}

// --- Properties ----------------------------------------------------------------

// playPair is a generated (series, card) that is a legal play.
type playPair struct {
	series []cribbage.Card
	card   cribbage.Card
}

// randomPlay builds a random legal play by dealing cards one at a time until the
// next card would pass 31, then using the last legal card as the play.
func randomPlay(rng *rand.Rand) playPair {
	deck := cribbage.Deck()
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })

	var series []cribbage.Card
	count := 0
	for _, card := range deck {
		if count+card.Rank.PipValue() > 31 {
			continue
		}
		// With some probability stop and treat this card as the play.
		if len(series) > 0 && rng.Intn(3) == 0 {
			return playPair{series: series, card: card}
		}
		series = append(series, card)
		count += card.Rank.PipValue()
	}
	// Fell through: split off the last card as the play.
	return playPair{series: series[:len(series)-1], card: series[len(series)-1]}
}

func TestPropertiesRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500_000; i++ {
		p := randomPlay(rng)

		res, err := Score(p.series, p.card)
		if err != nil {
			t.Fatalf("Score error on %v + %v: %v", p.series, p.card, err)
		}

		// Total agrees with Score and with the independent reference.
		if tot, _ := Total(p.series, p.card); tot != res.Total {
			t.Fatalf("Total %d != Score %d", tot, res.Total)
		}
		if ref := referenceTotal(p.series, p.card); ref != res.Total {
			t.Fatalf("reference %d != Score %d for %v + %v", ref, res.Total, p.series, p.card)
		}

		// Total == sum of combo points.
		sum := 0
		for _, c := range res.Combos() {
			sum += c.Points
		}
		if sum != res.Total {
			t.Fatalf("combo points %d != total %d", sum, res.Total)
		}

		// Value ranges.
		if res.Pair.Points != 0 && res.Pair.Points != 2 && res.Pair.Points != 6 && res.Pair.Points != 12 {
			t.Fatalf("pair points %d not in {0,2,6,12}", res.Pair.Points)
		}
		if res.Run.Points != 0 && res.Run.Points < 3 {
			t.Fatalf("run points %d invalid (must be 0 or >=3)", res.Run.Points)
		}
		if res.Count > 31 {
			t.Fatalf("count %d exceeds 31", res.Count)
		}

		// Suit invariance: changing every suit by the same permutation cannot
		// change a pegging score.
		perm := suitPerm(rng)
		ss := make([]cribbage.Card, len(p.series))
		for j, card := range p.series {
			ss[j] = cribbage.Card{Rank: card.Rank, Suit: perm[card.Suit]}
		}
		sc := cribbage.Card{Rank: p.card.Rank, Suit: perm[p.card.Suit]}
		if st, _ := Total(ss, sc); st != res.Total {
			t.Fatalf("suit permutation changed score: %d != %d", st, res.Total)
		}
	}
}

func suitPerm(rng *rand.Rand) [4]cribbage.Suit {
	p := [4]cribbage.Suit{cribbage.Clubs, cribbage.Diamonds, cribbage.Hearts, cribbage.Spades}
	rng.Shuffle(4, func(i, j int) { p[i], p[j] = p[j], p[i] })
	return p
}
