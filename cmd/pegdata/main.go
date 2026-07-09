// pegdata generates pegging self-play training data (docs/research/ml-bot
// chapter 4): full games where both seats discard like the champion and peg
// with the chosen behavior policy, one JSONL row per non-forced pegging
// decision, labeled with its Monte-Carlo return (own future pegging points
// minus the opponent's, to the end of the deal).
//
// Usage: pegdata [-games n] [-seed s] [-policy random|champion|net]
//                [-weights file] [-epsilon e] [-out file]
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
	policy := flag.String("policy", "random", "behavior policy: random, champion, or net")
	weights := flag.String("weights", "", "weights file (required for -policy net)")
	epsilon := flag.Float64("epsilon", 0.2, "exploration rate for -policy net")
	out := flag.String("out", "", "output file (default stdout)")
	flag.Parse()

	var pol peg.Policy
	switch *policy {
	case "random":
		pol = peg.Random{}
	case "champion":
		pol = peg.Champion{}
	case "net":
		m, err := nn.LoadFile(*weights)
		if err != nil {
			log.Fatal(err)
		}
		pol = peg.Net{M: m, Epsilon: *epsilon}
	default:
		log.Fatalf("unknown policy %q", *policy)
	}

	var w io.Writer = os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		w = f
	}

	st, err := peg.Generate(*games, *seed, pol, w)
	if err != nil {
		log.Fatal(err)
	}
	st.Finalize()
	fmt.Fprintf(os.Stderr, "%d games, %d deals, %d decisions; pegging pts/deal: dealer %.2f, pone %.2f\n",
		st.Games, st.Deals, st.Decisions, st.PegPerDeal[0], st.PegPerDeal[1])
}
