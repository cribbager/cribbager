package pegging

import (
	"github.com/cribbager/cribbager/internal/cribbage"
)

// referenceTotal is an independent, deliberately different implementation used
// only by tests. It must agree with the production scorer over every legal play.
// Where production detects a run with a bitmask, the reference sorts (insertion
// sort) and checks consecutive differences — a different algorithm, so a shared
// bug is unlikely. It is allocation-free so the exhaustive test stays fast.
func referenceTotal(series []cribbage.Card, card cribbage.Card) int {
	count := card.Rank.PipValue()
	for _, c := range series {
		count += c.Rank.PipValue()
	}

	total := 0
	if count == 15 {
		total += 2
	}
	if count == 31 {
		total += 2
	}

	// Pair: trailing cards equal in rank to the played card.
	k := 1
	for i := len(series) - 1; i >= 0 && series[i].Rank == card.Rank; i-- {
		k++
	}
	total += k * (k - 1)

	// Gather all ranks (series then card) into a stack buffer.
	var all [32]int
	n := 0
	for _, c := range series {
		all[n] = int(c.Rank)
		n++
	}
	all[n] = int(card.Rank)
	n++

	// Run: largest L>=3 (a run can't exceed 13 distinct ranks) whose last L ranks
	// sort into consecutive integers.
	start := n
	if start > 13 {
		start = 13
	}
	for l := start; l >= 3; l-- {
		var s [13]int
		copy(s[:l], all[n-l:n])
		// insertion sort s[:l]
		for a := 1; a < l; a++ {
			v, b := s[a], a-1
			for b >= 0 && s[b] > v {
				s[b+1] = s[b]
				b--
			}
			s[b+1] = v
		}
		consecutive := true
		for a := 1; a < l; a++ {
			if s[a] != s[a-1]+1 {
				consecutive = false
				break
			}
		}
		if consecutive {
			total += l
			break
		}
	}
	return total
}
