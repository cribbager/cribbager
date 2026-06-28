package hand

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// --- Golden vectors (Oracle D) -------------------------------------------------

type vector struct {
	Name    string   `json:"name"`
	Hand    []string `json:"hand"`
	Starter string   `json:"starter"`
	IsCrib  bool     `json:"isCrib"`
	Total   int      `json:"total"`
	Points  points   `json:"points"`
	RunLen  int      `json:"runLength"`
	RunMult int      `json:"multiplicity"`
}

type points struct {
	Fifteens int `json:"fifteens"`
	Pairs    int `json:"pairs"`
	Runs     int `json:"runs"`
	Flush    int `json:"flush"`
	Nobs     int `json:"nobs"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	path := filepath.Join("testdata", "hand_scores.json")
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

func (v vector) parse(t *testing.T) ([4]cribbage.Card, cribbage.Card) {
	t.Helper()
	if len(v.Hand) != 4 {
		t.Fatalf("%s: hand has %d cards, want 4", v.Name, len(v.Hand))
	}
	var h [4]cribbage.Card
	for i, s := range v.Hand {
		c, err := cribbage.ParseCard(s)
		if err != nil {
			t.Fatalf("%s: bad card %q: %v", v.Name, s, err)
		}
		h[i] = c
	}
	starter, err := cribbage.ParseCard(v.Starter)
	if err != nil {
		t.Fatalf("%s: bad starter %q: %v", v.Name, v.Starter, err)
	}
	return h, starter
}

func categorySums(r Result) points {
	var p points
	for _, c := range r.Fifteens {
		p.Fifteens += c.Points
	}
	for _, c := range r.Pairs {
		p.Pairs += c.Points
	}
	for _, c := range r.Runs {
		p.Runs += c.Points
	}
	p.Flush = r.Flush.Points
	p.Nobs = r.Nobs.Points
	return p
}

func TestGoldenVectors(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			h, starter := v.parse(t)

			res, err := Score(h, starter, v.IsCrib)
			if err != nil {
				t.Fatalf("Score error: %v", err)
			}
			if res.Total != v.Total {
				t.Errorf("Score total = %d, want %d", res.Total, v.Total)
			}
			if got := categorySums(res); got != v.Points {
				t.Errorf("category points = %+v, want %+v", got, v.Points)
			}

			// Total must agree with Score.
			tot, err := Total(h, starter, v.IsCrib)
			if err != nil {
				t.Fatalf("Total error: %v", err)
			}
			if tot != v.Total {
				t.Errorf("Total = %d, want %d", tot, v.Total)
			}

			// The independent reference must agree too.
			if ref := referenceTotal(h, starter, v.IsCrib); ref != v.Total {
				t.Errorf("referenceTotal = %d, want %d", ref, v.Total)
			}

			// Run structure, when the vector specifies it.
			if v.RunLen > 0 {
				if len(res.Runs) != 1 {
					t.Fatalf("expected 1 run combo, got %d", len(res.Runs))
				}
				run := res.Runs[0]
				if run.RunLength != v.RunLen || run.Multiplicity != v.RunMult {
					t.Errorf("run = {len %d, mult %d}, want {len %d, mult %d}",
						run.RunLength, run.Multiplicity, v.RunLen, v.RunMult)
				}
			} else if len(res.Runs) != 0 {
				t.Errorf("expected no run, got %+v", res.Runs)
			}
		})
	}
}

// --- Invalid input -------------------------------------------------------------

func TestRejectsDuplicateCard(t *testing.T) {
	dup, _ := cribbage.NewCard(5, cribbage.Hearts)
	other, _ := cribbage.NewCard(6, cribbage.Clubs)
	h := [4]cribbage.Card{dup, other, {Rank: 7, Suit: cribbage.Diamonds}, {Rank: 8, Suit: cribbage.Spades}}

	// Starter duplicates a hand card.
	if _, err := Score(h, dup, false); !errors.Is(err, ErrDuplicateCard) {
		t.Errorf("Score duplicate: err = %v, want ErrDuplicateCard", err)
	}
	if _, err := Total(h, dup, false); !errors.Is(err, ErrDuplicateCard) {
		t.Errorf("Total duplicate: err = %v, want ErrDuplicateCard", err)
	}
}

// --- Properties (Oracle C) -----------------------------------------------------

// randomDeal draws four hand cards and a starter from a shuffled deck.
func randomDeal(rng *rand.Rand) ([4]cribbage.Card, cribbage.Card) {
	deck := cribbage.Deck()
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	return [4]cribbage.Card{deck[0], deck[1], deck[2], deck[3]}, deck[4]
}

const impossible19, impossible25, impossible26, impossible27 = 19, 25, 26, 27

func TestPropertiesRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200_000; i++ {
		h, starter := randomDeal(rng)
		crib := i%2 == 0

		res, err := Score(h, starter, crib)
		if err != nil {
			t.Fatalf("Score error on %v + %v: %v", h, starter, err)
		}

		// Score range and the four impossible scores.
		if res.Total < 0 || res.Total > 29 {
			t.Fatalf("total %d out of [0,29] for %v + %v", res.Total, h, starter)
		}
		switch res.Total {
		case impossible19, impossible25, impossible26, impossible27:
			t.Fatalf("impossible score %d for %v + %v", res.Total, h, starter)
		}

		// Total == sum of combo points.
		sum := 0
		for _, c := range res.Combos() {
			sum += c.Points
		}
		if sum != res.Total {
			t.Fatalf("combo points %d != total %d for %v + %v", sum, res.Total, h, starter)
		}

		// Score and Total agree.
		tot, _ := Total(h, starter, crib)
		if tot != res.Total {
			t.Fatalf("Total %d != Score %d for %v + %v", tot, res.Total, h, starter)
		}

		// Reference agrees.
		if ref := referenceTotal(h, starter, crib); ref != res.Total {
			t.Fatalf("reference %d != Score %d for %v + %v", ref, res.Total, h, starter)
		}

		// Permuting the hand cards among themselves never changes the score.
		ph := [4]cribbage.Card{h[3], h[0], h[2], h[1]}
		if pt, _ := Total(ph, starter, crib); pt != res.Total {
			t.Fatalf("hand-order changed score: %d != %d", pt, res.Total)
		}

		// Relabeling all suits with any permutation preserves the score (flush
		// and nobs are relational).
		perm := suitPerm(rng)
		sh := [4]cribbage.Card{permuteSuit(h[0], perm), permuteSuit(h[1], perm), permuteSuit(h[2], perm), permuteSuit(h[3], perm)}
		ss := permuteSuit(starter, perm)
		if st, _ := Total(sh, ss, crib); st != res.Total {
			t.Fatalf("suit permutation changed score: %d != %d", st, res.Total)
		}
	}
}

func suitPerm(rng *rand.Rand) [4]cribbage.Suit {
	p := [4]cribbage.Suit{cribbage.Clubs, cribbage.Diamonds, cribbage.Hearts, cribbage.Spades}
	rng.Shuffle(4, func(i, j int) { p[i], p[j] = p[j], p[i] })
	return p
}

func permuteSuit(c cribbage.Card, perm [4]cribbage.Suit) cribbage.Card {
	return cribbage.Card{Rank: c.Rank, Suit: perm[c.Suit]}
}

func TestCribFlushNeverBeatsHand(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 50_000; i++ {
		h, starter := randomDeal(rng)
		hand, _ := Total(h, starter, false)
		crib, _ := Total(h, starter, true)
		if crib > hand {
			t.Fatalf("crib %d > hand %d for %v + %v", crib, hand, h, starter)
		}
	}
}
