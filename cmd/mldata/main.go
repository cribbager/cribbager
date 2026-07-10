// mldata deals random six-card cribbage hands and labels all 15 keep/discard
// splits of each with the champion's exact discard expectations, emitting one
// JSON line per hand. It is the training-data generator for the ML bot's
// supervised discard network (docs/research/ml-bot): the engine plus the exact
// evaluator act as a perfect, infinite teacher — every example carries the
// true expected value, not a noisy estimate.
//
// Each split records the exact expected show value of the kept four (ehand)
// and the exact expected crib value of the thrown two (crib_ev, unsigned).
// The trainer derives the target for either seat from one line — a dealer's
// total is ehand + crib_ev, the pone's is ehand − crib_ev — which halves the
// file and puts the dealer/pone symmetry where the trainer can see it.
//
// A second mode (chapter 7) trades exactness for completeness:
// -mode outcomes plays full games (champion discards with ε-random
// exploration, production ML pegging) and labels each CHOSEN split with the
// deal's realized net points — see outcomes.go.
//
// Usage: mldata [-mode exact|outcomes] [-n hands|games] [-seed s]
//
//	[-epsilon e] [-out file]
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"

	"github.com/cribbager/cribbager/internal/bot/eval"
	"github.com/cribbager/cribbager/internal/cribbage"
)

// split is one of a hand's 15 keep/discard choices with its exact labels.
type split struct {
	Keep    []string `json:"keep"`
	Discard []string `json:"discard"`
	EHand   float64  `json:"ehand"`
	CribEV  float64  `json:"crib_ev"`
}

// row is one dealt hand with all 15 labeled splits: one JSONL line.
type row struct {
	Hand   []string `json:"hand"`
	Splits []split  `json:"splits"`
}

func main() {
	n := flag.Int("n", 10000, "hands to deal (exact mode) or games to play (outcomes mode)")
	seed := flag.Int64("seed", 1, "RNG seed, so a dataset is reproducible")
	mode := flag.String("mode", "exact", "exact (evaluator-labeled splits) or outcomes (realized deal points)")
	epsilon := flag.Float64("epsilon", 0.25, "outcomes mode: fraction of uniformly random splits (exploration)")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()

	var w io.Writer = os.Stdout
	var f *os.File
	if *out != "" {
		var err error
		if f, err = os.Create(*out); err != nil {
			log.Fatal(err)
		}
		w = f
	}
	bw := bufio.NewWriter(w)

	var err error
	switch *mode {
	case "exact":
		err = generate(bw, *n, *seed)
	case "outcomes":
		err = generateOutcomes(bw, *n, *seed, *epsilon)
	case "win":
		err = generateWin(bw, *n, *seed, *epsilon)
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
	if err != nil {
		log.Fatal(err)
	}
	// Checked, not deferred: an unflushed buffer or failed close is silent
	// data loss in a dataset file.
	if err := bw.Flush(); err != nil {
		log.Fatal(err)
	}
	if f != nil {
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}
}

// generate deals n hands with a deterministic RNG and writes one labeled row
// per hand. RankDiscards is called with myCrib=true so the signed crib term it
// returns is the raw (unsigned) crib expectation.
func generate(w io.Writer, n int, seed int64) error {
	rng := rand.New(rand.NewSource(seed))
	enc := json.NewEncoder(w)
	deck := cribbage.Deck()
	for range n {
		rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
		var hand [6]cribbage.Card
		copy(hand[:], deck[:6])

		ranked := eval.RankDiscards(hand, true)
		r := row{Hand: cardStrings(hand[:]), Splits: make([]split, len(ranked))}
		for i, d := range ranked {
			r.Splits[i] = split{
				Keep:    cardStrings(d.Keep[:]),
				Discard: cardStrings(d.Discard[:]),
				EHand:   d.EHand,
				CribEV:  d.Crib,
			}
		}
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("writing row: %w", err)
		}
	}
	return nil
}

func cardStrings(cs []cribbage.Card) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}
