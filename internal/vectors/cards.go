package vectors

import (
	"fmt"
	"strings"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// The vector files use a language-neutral card encoding: rank then suit, where
// rank is A 2 3 4 5 6 7 8 9 10 J Q K and suit is C D H S. This differs from the
// engine's internal notation only in writing the ten as "10" rather than "T".

func enc(c cribbage.Card) string {
	r := c.Rank.String()
	if r == "T" {
		r = "10"
	}
	return r + c.Suit.String()
}

func encAll(cs []cribbage.Card) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = enc(c)
	}
	return out
}

// card parses the neutral encoding; it panics on bad input (generator tables
// are static, so a typo should stop the build, not skew a vector).
func card(s string) cribbage.Card {
	if strings.HasPrefix(s, "10") {
		s = "T" + s[2:]
	}
	c, err := cribbage.ParseCard(s)
	if err != nil {
		panic(fmt.Sprintf("vectors: bad card %q: %v", s, err))
	}
	return c
}

func cards(ss ...string) []cribbage.Card {
	out := make([]cribbage.Card, len(ss))
	for i, s := range ss {
		out[i] = card(s)
	}
	return out
}

func four(cs []cribbage.Card) [4]cribbage.Card {
	return [4]cribbage.Card{cs[0], cs[1], cs[2], cs[3]}
}
