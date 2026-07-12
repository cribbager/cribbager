package eval

import (
	"sync"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/hand"
)

// Opponent-modeled crib EV — an experimental alternative to the uniform-opponent
// crib table (CribEV / cribEVTable). The uniform table treats every completion
// of the crib as equally likely; a real opponent does not throw uniformly. To
// OUR crib (they are the pone) they throw DEFENSIVELY, keeping their good cards;
// to THEIR OWN crib (they are the dealer) they throw HELPFULLY. This weights the
// opponent's two crib cards by the role-aware discard model already used for
// pegging belief (keepProb — the probability the champion's own policy keeps a
// rank, which by self-play stands in for the opponent's), so the crib
// expectation reflects how the opponent actually discards. The starter stays
// uniform: nobody throws it.
//
// The result is built once, lazily, into oppCribTable[role][lo][hi][suited],
// mirroring cribEVTable's indexing. role 0 = opponent throws to our crib as the
// pone (defensive); role 1 = opponent throws to their own crib as the dealer
// (helpful).

var (
	oppCribTable [2][14][14][2]float64
	oppCribOnce  sync.Once
)

// throwWeight is the (unnormalized) probability the opponent throws a card of
// this rank to the crib in the given role — the complement of keepProb.
func throwWeight(role int, r cribbage.Rank) float64 { return 1 - keepProb[role][r] }

// weightedCrib is the exact expected crib value of throwing a and b, enumerating
// every completion (starter + opponent pair) from the 50 cards not discarded —
// the same sweep the uniform generator does — but weighting each opponent pair
// by the role-aware throw probability. With all weights equal to 1 it reduces to
// the uniform mean, which the test pins to CribEV (so this enumeration is
// verified to match the shipped table's).
func weightedCrib(a, b cribbage.Card, role int) float64 {
	rest := make([]cribbage.Card, 0, 50)
	for _, c := range cribbage.Deck() {
		if c != a && c != b {
			rest = append(rest, c)
		}
	}
	n := len(rest)
	var sum, wsum float64
	for s := 0; s < n; s++ {
		starter := rest[s]
		for i := 0; i < n; i++ {
			if i == s {
				continue
			}
			wi := throwWeight(role, rest[i].Rank)
			for j := i + 1; j < n; j++ {
				if j == s {
					continue
				}
				w := wi * throwWeight(role, rest[j].Rank)
				crib := [4]cribbage.Card{a, b, rest[i], rest[j]}
				t, _ := hand.Total(crib, starter, true)
				sum += w * float64(t)
				wsum += w
			}
		}
	}
	if wsum == 0 {
		return 0
	}
	return sum / wsum
}

func buildOppCribTable() {
	cardOf := func(rank, suit int) cribbage.Card {
		return cribbage.Card{Rank: cribbage.Rank(rank), Suit: cribbage.Suit(suit)}
	}
	for role := 0; role < 2; role++ {
		for lo := 1; lo <= 13; lo++ {
			for hi := lo; hi <= 13; hi++ {
				if lo == hi {
					oppCribTable[role][lo][hi][0] = weightedCrib(cardOf(lo, 0), cardOf(lo, 1), role)
				} else {
					oppCribTable[role][lo][hi][0] = weightedCrib(cardOf(lo, 0), cardOf(hi, 1), role)
					oppCribTable[role][lo][hi][1] = weightedCrib(cardOf(lo, 0), cardOf(hi, 0), role)
				}
			}
		}
	}
}

// OppCribEV is the opponent-modeled counterpart of CribEV: the expected crib
// value (unsigned) of throwing a and b, weighting the opponent's contribution by
// the role-aware discard model instead of uniformly. myCrib true = it is our
// crib (we deal; the opponent throws to it as the pone, defensively); false = it
// is the opponent's crib (they deal; they throw to it helpfully). Callers apply
// the +own / −opponent sign, exactly as with CribEV.
func OppCribEV(a, b cribbage.Card, myCrib bool) float64 {
	oppCribOnce.Do(buildOppCribTable)
	role := 1 // opponent throws to their own crib (we are the pone)
	if myCrib {
		role = 0 // opponent throws to our crib as the pone (defensive)
	}
	lo, hi := int(a.Rank), int(b.Rank)
	if lo > hi {
		lo, hi = hi, lo
	}
	suited := 0
	if a.Suit == b.Suit && lo != hi {
		suited = 1
	}
	return oppCribTable[role][lo][hi][suited]
}
