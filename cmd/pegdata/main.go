// pegdata generates pegging self-play training data (docs/research/ml-bot
// chapter 4): full games where both seats discard like the champion and peg
// with the chosen behavior policy, one JSONL row per non-forced pegging
// decision, labeled with its Monte-Carlo return (own future pegging points
// minus the opponent's, to the end of the deal).
//
// Usage: pegdata [-games n] [-seed s] [-policy random|champion|net]
//
//	[-weights file] [-epsilon e] [-out file]
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cribbager/cribbager/internal/bot/lab/peg"
	"github.com/cribbager/cribbager/internal/nn"
)

func main() {
	games := flag.Int("games", 20000, "number of self-play games")
	seed := flag.Int64("seed", 1, "RNG seed")
	policy := flag.String("policy", "random", "seat-0 behavior policy: random, champion, or net")
	opponent := flag.String("opponent", "same", "seat-1 policy: same, random, champion, or net")
	weights := flag.String("weights", "", "weights file (required for a net policy)")
	epsilon := flag.Float64("epsilon", 0.2, "exploration rate for -policy net (opponent net plays greedy)")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()

	build := func(name string, eps float64) peg.Policy {
		switch name {
		case "random":
			return peg.Random{}
		case "champion":
			return peg.Champion{}
		case "net":
			m, err := nn.LoadFile(*weights)
			if err != nil {
				log.Fatal(err)
			}
			return peg.Net{M: m, Epsilon: eps}
		default:
			log.Fatalf("unknown policy %q", name)
			return nil
		}
	}
	pols := [2]peg.Policy{build(*policy, *epsilon), nil}
	if *opponent == "same" {
		pols[1] = pols[0]
	} else {
		pols[1] = build(*opponent, 0)
	}

	var w io.Writer = os.Stdout
	var f *os.File
	if *out != "" {
		var err error
		if f, err = os.Create(*out); err != nil {
			log.Fatal(err)
		}
		w = f
	}

	st, err := peg.Generate(*games, *seed, pols, w)
	if err != nil {
		log.Fatal(err)
	}
	// Checked, not deferred: a failed close is silent data loss in a dataset.
	if f != nil {
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
	}
	st.Finalize()
	fmt.Fprintf(os.Stderr, "%d games, %d deals, %d decisions; pegging pts/deal: dealer %.2f, pone %.2f\n",
		st.Games, st.Deals, st.Decisions, st.PegPerDeal[0], st.PegPerDeal[1])
}
