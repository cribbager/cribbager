// Command cribbager is the command-line client: an interactive game against the
// bot, plus the hand/pegging scorers as live harnesses.
//
//	cribbager play                         # play a game against a bot
//	cribbager score 5C 5D 5H JS 5S         # hand + starter (last card is the cut)
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "score":
		if err := runScore(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "peg":
		if err := runPeg(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "play":
		if err := cmdPlay(os.Args[2:]); err != nil && err != errQuit {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cribbager — cribbage command-line client

usage:
  cribbager play    [--bot champion|random] [--seed N]
  cribbager score   C1 C2 C3 C4 STARTER [--crib]
  cribbager peg     C1 C2 C3 ...

cards are rank (A 2-9 T J Q K) + suit (C D H S), e.g. 5H, TD, JS — "10" also works for T (10C = TC)

  play:    an interactive game against the bot (default champion).
  score:   the four hand cards then the starter (cut) card.
  peg:     cards played during one count sequence, in order; each play's points
           and the running count are shown. (Resets at 31 belong to the game
           engine; pass cards from a single sequence.)
`)
}

// runPeg replays a single pegging count-sequence, scoring each card as it is
// played against the cards before it. It is a harness for the pegging scorer.
func runPeg(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("need at least one card")
	}
	cards := make([]cribbage.Card, len(args))
	for i, s := range args {
		c, err := parseCardArg(s)
		if err != nil {
			return err
		}
		cards[i] = c
	}

	running := 0
	for i, card := range cards {
		res, err := pegging.Score(cards[:i], card)
		if err != nil {
			return fmt.Errorf("playing %s: %w", card, err)
		}
		running = res.Count
		line := fmt.Sprintf("  play %s  ->  count %2d", card, running)
		for _, combo := range res.Combos() {
			line += fmt.Sprintf("   %s", pegDescribe(combo))
		}
		fmt.Println(line)
	}
	return nil
}

func pegDescribe(c pegging.Combo) string {
	switch c.Kind {
	case pegging.Fifteen:
		return "fifteen for 2"
	case pegging.ThirtyOne:
		return "thirty-one for 2"
	case pegging.Pair:
		return fmt.Sprintf("%s for %d", pegPairName(c.Points), c.Points)
	case pegging.Run:
		return fmt.Sprintf("run of %d for %d", c.RunLength, c.Points)
	default:
		return fmt.Sprintf("%v for %d", c.Kind, c.Points)
	}
}

func pegPairName(points int) string {
	switch points {
	case 6:
		return "pair royal"
	case 12:
		return "double pair royal"
	default:
		return "pair"
	}
}

func runScore(args []string) error {
	isCrib := false
	var cardArgs []string
	for _, a := range args {
		switch a {
		case "--crib":
			isCrib = true
		default:
			cardArgs = append(cardArgs, a)
		}
	}
	if len(cardArgs) != 5 {
		return fmt.Errorf("need 4 hand cards and a starter (got %d cards)", len(cardArgs))
	}

	var cards [5]cribbage.Card
	for i, s := range cardArgs {
		c, err := parseCardArg(s)
		if err != nil {
			return err
		}
		cards[i] = c
	}
	hcards := [4]cribbage.Card{cards[0], cards[1], cards[2], cards[3]}
	starter := cards[4]

	res, err := hand.Score(hcards, starter, isCrib)
	if err != nil {
		return err
	}

	kind := "hand"
	if isCrib {
		kind = "crib"
	}
	fmt.Printf("%s  +  %s (cut)   [%s]\n", cardsStr(hcards[:]), starter, kind)
	combos := res.Combos()
	if len(combos) == 0 {
		fmt.Println("  (nothing scores)")
	}
	for _, c := range combos {
		fmt.Printf("  %-26s %s\n", describe(c), cardsStr(c.Cards))
	}
	fmt.Printf("  %-26s\n", strings.Repeat("-", 20))
	fmt.Printf("  total: %d\n", res.Total)
	return nil
}

// describe renders a combo as the phrase a player would say, derived entirely
// from the structured fields — the engine itself stays presentation-free.
func describe(c hand.Combo) string {
	switch c.Kind {
	case hand.Fifteen:
		return fmt.Sprintf("fifteen for %d", c.Points)
	case hand.Pair:
		return fmt.Sprintf("pair for %d", c.Points)
	case hand.Run:
		return fmt.Sprintf("%s of %d for %d", runName(c.Multiplicity), c.RunLength, c.Points)
	case hand.Flush:
		return fmt.Sprintf("flush for %d", c.Points)
	case hand.Nobs:
		return "nobs for 1"
	default:
		return fmt.Sprintf("%v for %d", c.Kind, c.Points)
	}
}

func runName(multiplicity int) string {
	switch multiplicity {
	case 2:
		return "double run"
	case 3:
		return "triple run"
	case 4:
		return "double-double run"
	default:
		return "run"
	}
}

func cardsStr(cs []cribbage.Card) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c.String()
	}
	return strings.Join(parts, " ")
}
