package hand

import (
	"math/bits"
	"sort"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// referenceTotal is a deliberately independent, brute-force scorer used only by
// tests. It must agree with the production Total over the entire input space.
//
// Independence is the point: it uses a different algorithm wherever it can so a
// shared bug is unlikely to fool both. Runs in particular are scored by
// enumerating subsets and finding the longest run, rather than by the
// production count-array scan.
func referenceTotal(h [4]cribbage.Card, starter cribbage.Card, isCrib bool) int {
	cards := [5]cribbage.Card{h[0], h[1], h[2], h[3], starter}
	total := 0

	// Fifteens: brute force over all subsets.
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

	// Pairs: every unordered pair of equal rank.
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			if cards[i].Rank == cards[j].Rank {
				total += 2
			}
		}
	}

	// Runs: find the largest subset size that forms at least one run, then count
	// how many subsets of that size are runs. That count is the multiplicity.
	for size := 5; size >= 3; size-- {
		runs := 0
		for mask := 1; mask < 32; mask++ {
			if bits.OnesCount(uint(mask)) != size {
				continue
			}
			ranks := make([]int, 0, size)
			for i := 0; i < 5; i++ {
				if mask&(1<<i) != 0 {
					ranks = append(ranks, int(cards[i].Rank))
				}
			}
			if isRun(ranks) {
				runs++
			}
		}
		if runs > 0 {
			total += size * runs
			break
		}
	}

	// Flush.
	if h[0].Suit == h[1].Suit && h[1].Suit == h[2].Suit && h[2].Suit == h[3].Suit {
		switch {
		case starter.Suit == h[0].Suit:
			total += 5
		case !isCrib:
			total += 4
		}
	}

	// Nobs.
	for _, c := range h {
		if c.Rank == cribbage.Jack && c.Suit == starter.Suit {
			total++
		}
	}
	return total
}

// isRun reports whether the ranks are all distinct and consecutive (Ace low, no
// wraparound).
func isRun(ranks []int) bool {
	rs := append([]int(nil), ranks...)
	sort.Ints(rs)
	for i := 1; i < len(rs); i++ {
		if rs[i] != rs[i-1]+1 {
			return false
		}
	}
	return true
}
