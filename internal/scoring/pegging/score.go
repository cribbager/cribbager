// Package pegging scores a single card played during the play (pegging) phase
// of cribbage: given the cards already played in the current count sequence and
// the card just played, it returns the points that play scores.
//
// It scores the four play events driven by a card: reaching fifteen, reaching
// thirty-one, pairs (pair / pair royal / double pair royal), and runs. It does
// NOT award the 1-point "go" or "last card" — those depend on whose turn it is
// and whether a player can play at all, which is game state. The game engine
// owns the count resets at 31 and the go/last-card awards, and calls this
// scorer for each play.
//
// Pegging scores are independent of suit; suit matters only to keep the
// physical cards distinct. Both that suit-independence and the rules below are
// checked exhaustively over every legal rank sequence in the tests.
//
// Like the hand scorer, there are two entry points: Total (lean, score only)
// and Score (itemized breakdown). Both reject illegal plays.
package pegging

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// Kind tags what a Combo scored.
type Kind uint8

const (
	Fifteen Kind = iota
	ThirtyOne
	Pair
	Run
)

func (k Kind) String() string {
	switch k {
	case Fifteen:
		return "fifteen"
	case ThirtyOne:
		return "thirty-one"
	case Pair:
		return "pair"
	case Run:
		return "run"
	default:
		return "?"
	}
}

// Combo is one scoring element of a play. RunLength is meaningful only when
// Kind is Run (and equals Points, since a pegging run of length L scores L).
type Combo struct {
	Kind      Kind
	Cards     []cribbage.Card
	Points    int
	RunLength int // Run only
}

// Result is the itemized breakdown of a single play. At most one of each kind
// can occur on a play, so each is a single field. Total is the sum of their
// points; Count is the running count after the card is played.
type Result struct {
	Total     int
	Count     int
	Fifteen   Combo
	ThirtyOne Combo
	Pair      Combo
	Run       Combo
}

// Combos flattens the non-empty combos in canonical order.
func (r Result) Combos() []Combo {
	out := make([]Combo, 0, 4)
	for _, c := range []Combo{r.Fifteen, r.ThirtyOne, r.Pair, r.Run} {
		if c.Points > 0 {
			out = append(out, c)
		}
	}
	return out
}

// Errors returned for illegal plays — the "no bad input" boundary.
var (
	ErrCountExceeds31 = errors.New("pegging: play would take the count past 31")
	ErrDuplicateCard  = errors.New("pegging: the same card cannot be played twice")
)

// RunningCount sums the pip values of the cards played so far.
func RunningCount(series []cribbage.Card) int {
	n := 0
	for _, c := range series {
		n += c.Rank.PipValue()
	}
	return n
}

// Total returns the points a play scores, without building a breakdown. It does
// not allocate on the success path.
func Total(series []cribbage.Card, card cribbage.Card) (int, error) {
	if err := validate(series, card); err != nil {
		return 0, err
	}
	return totalCore(series, card), nil
}

// Score returns the points plus the itemized breakdown.
func Score(series []cribbage.Card, card cribbage.Card) (Result, error) {
	if err := validate(series, card); err != nil {
		return Result{}, err
	}

	all := make([]cribbage.Card, len(series)+1)
	copy(all, series)
	all[len(series)] = card

	var res Result
	res.Count = RunningCount(series) + card.Rank.PipValue()

	if res.Count == 15 {
		res.Fifteen = Combo{Kind: Fifteen, Cards: clone(all), Points: 2}
	}
	if res.Count == 31 {
		res.ThirtyOne = Combo{Kind: ThirtyOne, Cards: clone(all), Points: 2}
	}

	if k := trailingSameRank(all); k >= 2 {
		pairCards := clone(all[len(all)-k:])
		sortBySuit(pairCards)
		res.Pair = Combo{Kind: Pair, Cards: pairCards, Points: k * (k - 1)}
	}

	if l, cards := longestRunSuffix(all); l >= 3 {
		sortByRank(cards)
		res.Run = Combo{Kind: Run, Cards: cards, Points: l, RunLength: l}
	}

	for _, c := range res.Combos() {
		res.Total += c.Points
	}
	return res, nil
}

// totalCore is the lean scorer. It uses a stack-allocated rank buffer so the hot
// path (bots, the exhaustive test) does not touch the heap.
func totalCore(series []cribbage.Card, card cribbage.Card) int {
	count := RunningCount(series) + card.Rank.PipValue()
	total := 0
	if count == 15 {
		total += 2
	}
	if count == 31 {
		total += 2
	}

	// Pair: trailing cards sharing the played card's rank.
	k := 1
	for i := len(series) - 1; i >= 0 && series[i].Rank == card.Rank; i-- {
		k++
	}
	total += k * (k - 1)

	// Run: the longest suffix (>=3 cards) of distinct consecutive ranks.
	var buf [32]int
	m := 0
	for _, c := range series {
		buf[m] = int(c.Rank)
		m++
	}
	buf[m] = int(card.Rank)
	m++
	for l := m; l >= 3; l-- {
		if isRun(buf[m-l : m]) {
			total += l
			break
		}
	}
	return total
}

func validate(series []cribbage.Card, card cribbage.Card) error {
	for i := 0; i < len(series); i++ {
		if series[i] == card {
			return fmt.Errorf("%w: %s", ErrDuplicateCard, card)
		}
		for j := i + 1; j < len(series); j++ {
			if series[i] == series[j] {
				return fmt.Errorf("%w: %s", ErrDuplicateCard, series[i])
			}
		}
	}
	if RunningCount(series)+card.Rank.PipValue() > 31 {
		return fmt.Errorf("%w: %d + %d", ErrCountExceeds31, RunningCount(series), card.Rank.PipValue())
	}
	return nil
}

// isRun reports whether the ranks are all distinct and form consecutive
// integers (order-independent). Uses a bitmask, distinct from the reference's
// sort-based check.
func isRun(ranks []int) bool {
	var mask uint16
	lo, hi := 99, -1
	for _, r := range ranks {
		bit := uint16(1) << uint(r)
		if mask&bit != 0 {
			return false // duplicate rank
		}
		mask |= bit
		if r < lo {
			lo = r
		}
		if r > hi {
			hi = r
		}
	}
	return hi-lo == len(ranks)-1
}

func trailingSameRank(all []cribbage.Card) int {
	k := 0
	last := all[len(all)-1].Rank
	for i := len(all) - 1; i >= 0 && all[i].Rank == last; i-- {
		k++
	}
	return k
}

// longestRunSuffix returns the length and cards of the longest suffix (>=3) that
// forms a run, or (0, nil) if none.
func longestRunSuffix(all []cribbage.Card) (int, []cribbage.Card) {
	for l := len(all); l >= 3; l-- {
		suffix := all[len(all)-l:]
		ranks := make([]int, l)
		for i, c := range suffix {
			ranks[i] = int(c.Rank)
		}
		if isRun(ranks) {
			return l, clone(suffix)
		}
	}
	return 0, nil
}

func clone(cs []cribbage.Card) []cribbage.Card {
	out := make([]cribbage.Card, len(cs))
	copy(out, cs)
	return out
}

func sortByRank(cs []cribbage.Card) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Rank != cs[j].Rank {
			return cs[i].Rank < cs[j].Rank
		}
		return cs[i].Suit < cs[j].Suit
	})
}

func sortBySuit(cs []cribbage.Card) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Suit < cs[j].Suit })
}
