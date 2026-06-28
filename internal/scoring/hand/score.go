// Package hand scores a cribbage show: four cards in hand plus the starter
// (cut) card, for either a normal hand or the crib. Scoring is a pure, total
// function over five distinct valid cards.
//
// Two entry points share one definition of the rules:
//
//   - Total returns just the integer score. It allocates nothing and is the
//     right call for bots evaluating many positions.
//   - Score returns the same total plus an itemized, teachable breakdown.
//
// Score is the only place that builds the breakdown; Total and Score are
// cross-checked against each other and against an independent reference
// implementation over the entire input space (see the tests).
//
// Run bundling. A run with duplicate ranks (a "double run", "triple run",
// "double-double run") is reported as a SINGLE Run combo whose Points already
// include the pair(s) the duplication creates; those pairs are not emitted
// again as standalone Pair combos. The teachable split is still recoverable
// from the combo: the run portion is RunLength*Multiplicity and the remaining
// points are the absorbed pairs. A 5-card hand can hold at most one run, so
// Runs holds zero or one element.
package hand

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
	Pair
	Run
	Flush
	Nobs
)

func (k Kind) String() string {
	switch k {
	case Fifteen:
		return "fifteen"
	case Pair:
		return "pair"
	case Run:
		return "run"
	case Flush:
		return "flush"
	case Nobs:
		return "nobs"
	default:
		return "?"
	}
}

// Combo is one scoring element, structured rather than prose so the UI owns
// presentation and localization. RunLength and Multiplicity are meaningful only
// when Kind is Run (zero otherwise).
type Combo struct {
	Kind         Kind
	Cards        []cribbage.Card
	Points       int
	RunLength    int // Run only: length of the run, e.g. 3
	Multiplicity int // Run only: number of distinct runs (2=double, 3=triple, 4=double-double)
}

// Result is the itemized breakdown. Total equals the sum of every combo's
// points across all categories.
type Result struct {
	Total    int
	Fifteens []Combo
	Pairs    []Combo
	Runs     []Combo // zero or one element for a 5-card hand
	Flush    Combo   // Points == 0 when there is no flush
	Nobs     Combo   // Points == 0 when there is no nobs
}

// Combos flattens the breakdown into canonical order (fifteens, pairs, runs,
// flush, nobs), omitting empty flush/nobs. Handy for rendering and tests.
func (r Result) Combos() []Combo {
	out := make([]Combo, 0, len(r.Fifteens)+len(r.Pairs)+len(r.Runs)+2)
	out = append(out, r.Fifteens...)
	out = append(out, r.Pairs...)
	out = append(out, r.Runs...)
	if r.Flush.Points > 0 {
		out = append(out, r.Flush)
	}
	if r.Nobs.Points > 0 {
		out = append(out, r.Nobs)
	}
	return out
}

// ErrDuplicateCard is returned when the four hand cards and the starter are not
// all distinct — a situation that cannot arise from a real deal. It is the
// second half of "no bad input": NewCard guards each card's range, and the
// scorer guards their distinctness.
var ErrDuplicateCard = errors.New("hand: cards must be distinct")

// Total returns the score without building a breakdown. It allocates nothing on
// the success path.
func Total(handCards [4]cribbage.Card, starter cribbage.Card, isCrib bool) (int, error) {
	cards := fiveOf(handCards, starter)
	if err := checkDistinct(cards); err != nil {
		return 0, err
	}
	return totalOf(cards, handCards, starter, isCrib), nil
}

// Score returns the total plus the itemized breakdown.
func Score(handCards [4]cribbage.Card, starter cribbage.Card, isCrib bool) (Result, error) {
	cards := fiveOf(handCards, starter)
	if err := checkDistinct(cards); err != nil {
		return Result{}, err
	}

	var res Result
	res.Fifteens = scoreFifteens(cards)
	runCombo, inRun := scoreRun(cards)
	res.Pairs = scorePairs(cards, inRun)
	if runCombo.Points > 0 {
		res.Runs = []Combo{runCombo}
	}
	res.Flush = scoreFlush(handCards, starter, isCrib)
	res.Nobs = scoreNobs(handCards, starter)

	for _, c := range res.Combos() {
		res.Total += c.Points
	}
	return res, nil
}

func fiveOf(h [4]cribbage.Card, starter cribbage.Card) [5]cribbage.Card {
	return [5]cribbage.Card{h[0], h[1], h[2], h[3], starter}
}

func checkDistinct(cards [5]cribbage.Card) error {
	for i := 0; i < len(cards); i++ {
		for j := i + 1; j < len(cards); j++ {
			if cards[i] == cards[j] {
				return fmt.Errorf("%w: %s appears twice", ErrDuplicateCard, cards[i])
			}
		}
	}
	return nil
}

// totalOf is the lean, allocation-free scorer. It counts pairs traditionally
// (every same-rank pair) and runs as length*multiplicity; the sum equals
// Score's bundled total because Score merely regroups the same points.
func totalOf(cards [5]cribbage.Card, h [4]cribbage.Card, starter cribbage.Card, isCrib bool) int {
	total := 0

	// Fifteens: every subset summing to 15. A singleton tops out at 10, so it
	// can never reach 15 — no need to exclude singletons explicitly.
	for mask := 1; mask < 32; mask++ {
		sum := 0
		for i := 0; i < 5; i++ {
			if mask&(1<<i) != 0 {
				sum += cards[i].Rank.PipValue()
			}
		}
		if sum == 15 {
			total += 2
		}
	}

	var counts [14]int // index by rank 1..13
	for _, c := range cards {
		counts[c.Rank]++
	}

	// Pairs: 2 points per unordered same-rank pair => count*(count-1).
	for r := 1; r <= 13; r++ {
		total += counts[r] * (counts[r] - 1)
	}

	// Runs: each maximal run of >=3 consecutive ranks scores length*multiplicity.
	for r := 1; r <= 13; {
		if counts[r] == 0 {
			r++
			continue
		}
		start := r
		for r+1 <= 13 && counts[r+1] > 0 {
			r++
		}
		length := r - start + 1
		if length >= 3 {
			mult := 1
			for k := start; k <= r; k++ {
				mult *= counts[k]
			}
			total += length * mult
		}
		r++
	}

	total += flushPoints(h, starter, isCrib)
	if nobsPoint(h, starter) {
		total++
	}
	return total
}

func scoreFifteens(cards [5]cribbage.Card) []Combo {
	var combos []Combo
	for mask := 1; mask < 32; mask++ {
		sum := 0
		var cs []cribbage.Card
		for i := 0; i < 5; i++ {
			if mask&(1<<i) != 0 {
				sum += cards[i].Rank.PipValue()
				cs = append(cs, cards[i])
			}
		}
		if sum == 15 {
			sortCards(cs)
			combos = append(combos, Combo{Kind: Fifteen, Cards: cs, Points: 2})
		}
	}
	sortCombos(combos)
	return combos
}

// scoreRun finds the single maximal run (length >= 3) and bundles its inherent
// pairs into the run's points. It returns the ranks covered by the run so that
// scorePairs can skip them (no double counting).
func scoreRun(cards [5]cribbage.Card) (Combo, map[cribbage.Rank]bool) {
	var counts [14]int
	for _, c := range cards {
		counts[c.Rank]++
	}

	bestStart, bestLen := 0, 0
	for r := 1; r <= 13; {
		if counts[r] == 0 {
			r++
			continue
		}
		start := r
		for r+1 <= 13 && counts[r+1] > 0 {
			r++
		}
		if length := r - start + 1; length >= 3 && length > bestLen {
			bestStart, bestLen = start, length
		}
		r++
	}
	if bestLen < 3 {
		return Combo{}, nil
	}

	mult := 1
	pairPoints := 0
	inRun := make(map[cribbage.Rank]bool, bestLen)
	var runCards []cribbage.Card
	for rk := bestStart; rk < bestStart+bestLen; rk++ {
		c := counts[rk]
		mult *= c
		pairPoints += c * (c - 1) // pairs created by duplicate ranks within the run
		inRun[cribbage.Rank(rk)] = true
		for _, cd := range cards {
			if int(cd.Rank) == rk {
				runCards = append(runCards, cd)
			}
		}
	}
	sortCards(runCards)
	return Combo{
		Kind:         Run,
		Cards:        runCards,
		Points:       bestLen*mult + pairPoints,
		RunLength:    bestLen,
		Multiplicity: mult,
	}, inRun
}

// scorePairs emits standalone pairs only — pairs whose rank participates in the
// run are already counted inside the run combo.
func scorePairs(cards [5]cribbage.Card, inRun map[cribbage.Rank]bool) []Combo {
	byRank := map[cribbage.Rank][]cribbage.Card{}
	for _, c := range cards {
		byRank[c.Rank] = append(byRank[c.Rank], c)
	}
	var combos []Combo
	for rank, cs := range byRank {
		if inRun[rank] || len(cs) < 2 {
			continue
		}
		for i := 0; i < len(cs); i++ {
			for j := i + 1; j < len(cs); j++ {
				pair := []cribbage.Card{cs[i], cs[j]}
				sortCards(pair)
				combos = append(combos, Combo{Kind: Pair, Cards: pair, Points: 2})
			}
		}
	}
	sortCombos(combos)
	return combos
}

func scoreFlush(h [4]cribbage.Card, starter cribbage.Card, isCrib bool) Combo {
	pts := flushPoints(h, starter, isCrib)
	if pts == 0 {
		return Combo{}
	}
	cs := []cribbage.Card{h[0], h[1], h[2], h[3]}
	if pts == 5 {
		cs = append(cs, starter)
	}
	sortCards(cs)
	return Combo{Kind: Flush, Cards: cs, Points: pts}
}

// flushPoints implements the asymmetry between hand and crib: a hand scores 4
// for four matching cards (5 if the starter matches too); the crib scores only
// when all five match.
func flushPoints(h [4]cribbage.Card, starter cribbage.Card, isCrib bool) int {
	s := h[0].Suit
	if h[1].Suit != s || h[2].Suit != s || h[3].Suit != s {
		return 0
	}
	if starter.Suit == s {
		return 5
	}
	if isCrib {
		return 0
	}
	return 4
}

func scoreNobs(h [4]cribbage.Card, starter cribbage.Card) Combo {
	if c, ok := nobsCard(h, starter); ok {
		return Combo{Kind: Nobs, Cards: []cribbage.Card{c}, Points: 1}
	}
	return Combo{}
}

func nobsPoint(h [4]cribbage.Card, starter cribbage.Card) bool {
	_, ok := nobsCard(h, starter)
	return ok
}

func nobsCard(h [4]cribbage.Card, starter cribbage.Card) (cribbage.Card, bool) {
	for _, c := range h {
		if c.Rank == cribbage.Jack && c.Suit == starter.Suit {
			return c, true
		}
	}
	return cribbage.Card{}, false
}

func sortCards(cs []cribbage.Card) {
	sort.Slice(cs, func(i, j int) bool { return lessCard(cs[i], cs[j]) })
}

func lessCard(a, b cribbage.Card) bool {
	if a.Rank != b.Rank {
		return a.Rank < b.Rank
	}
	return a.Suit < b.Suit
}

// sortCombos imposes a deterministic order so golden vectors are stable: by the
// combos' cards, compared element by element.
func sortCombos(cs []Combo) {
	sort.Slice(cs, func(i, j int) bool { return lessCardSlice(cs[i].Cards, cs[j].Cards) })
}

func lessCardSlice(a, b []cribbage.Card) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return lessCard(a[i], b[i])
		}
	}
	return len(a) < len(b)
}
