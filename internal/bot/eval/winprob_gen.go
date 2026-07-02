//go:build ignore

// This program regenerates winprob.go: the win-probability table
// W[dealerScore][poneScore] = P(the player about to DEAL wins the game), plus
// the per-deal outcome marginals the win-aware discard objective needs.
//
// Method: champion self-play produces per-deal scoring sequences in counting
// order (heels, the pegging interleave, pone hand, dealer hand, crib — the
// engine's event order IS the counting order). The table is then exact dynamic
// programming over those observed sequences: from state (sD, sP), walk each
// weighted sequence; whoever crosses the target first wins, and if nobody does,
// the deal rotates — 1 − W[sP'][sD']. Every deal scores at least one point, so
// states can be evaluated in strictly decreasing order of sD+sP with no
// fixed-point iteration.
//
// Two evaluation tiers keep it fast: when either player is within reach of the
// target (their need ≤ the largest gain ever observed for their role), the
// crossing ORDER inside the deal matters, so sequences are walked via
// first-crossing indices; otherwise only the total gains matter and a joint
// (dealerGain, poneGain) histogram suffices.
//
// Caveat (accepted for v1, see docs/research/next-steps.md): the sequences come
// from score-blind self-play. After a win-aware champion is promoted, regenerate
// and confirm the table barely moves.
//
// Not part of the package build; regenerate with:
//
//	go generate ./internal/bot/eval
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"math"
	"os"
	"time"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/game"
)

const (
	outPath = "winprob.go"
	games   = 20_000
	target  = 121
)

// seq is one deduplicated deal outcome: the counting-order increments collapsed
// to what the DP needs — first-crossing index per possible need, total gains,
// and the observation weight.
type seq struct {
	w      float64
	gD, gP int     // total gains
	firstD []int32 // firstD[need-1]: 1-based increment index where dealer's cum ≥ need; -1 never
	firstP []int32
}

func main() {
	start := time.Now()

	// --- collect ---------------------------------------------------------------
	type incr struct {
		dealerSide bool
		pts        int
	}
	counts := map[string]float64{}
	decoded := map[string][]incr{}

	var handDist [2][30]float64 // [0]=pone hand, [1]=dealer hand
	var pegJoint = map[[2]int]float64{}
	var heelsDeals, totalDeals float64

	for g := 0; g < games; g++ {
		_, events, err := bot.PlayGameEvents(bot.Champion(), bot.Champion(), game.NewSeededDeck(int64(g)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "self-play: %v\n", err)
			os.Exit(1)
		}
		for _, d := range bot.DealStats(events) {
			if !d.Complete {
				continue
			}
			var key []byte
			var is []incr
			for _, in := range d.Increments {
				ds := in.Seat == d.Dealer
				b := byte(in.Pts)
				if ds {
					b |= 0x80
				}
				key = append(key, b)
				is = append(is, incr{dealerSide: ds, pts: in.Pts})
			}
			counts[string(key)]++
			decoded[string(key)] = is

			handDist[0][d.Hand[1-d.Dealer]]++
			handDist[1][d.Hand[d.Dealer]]++
			pegJoint[[2]int{d.Peg[d.Dealer], d.Peg[1-d.Dealer]}]++
			if d.Heels > 0 {
				heelsDeals++
			}
			totalDeals++
		}
		if g%2000 == 0 {
			fmt.Fprintf(os.Stderr, "\r  self-play… %d/%d (%.0fs)", g, games, time.Since(start).Seconds())
		}
	}
	fmt.Fprintf(os.Stderr, "\r  self-play done: %d deals, %d distinct sequences (%.0fs)\n",
		int(totalDeals), len(counts), time.Since(start).Seconds())

	// --- preprocess sequences ---------------------------------------------------
	maxGD, maxGP := 0, 0
	var seqs []seq
	for key, w := range counts {
		is := decoded[key]
		var s seq
		s.w = w / totalDeals
		for _, in := range is {
			if in.dealerSide {
				s.gD += in.pts
			} else {
				s.gP += in.pts
			}
		}
		s.firstD = make([]int32, s.gD)
		s.firstP = make([]int32, s.gP)
		for i := range s.firstD {
			s.firstD[i] = -1
		}
		for i := range s.firstP {
			s.firstP[i] = -1
		}
		cd, cp := 0, 0
		for idx, in := range is {
			if in.dealerSide {
				for n := cd; n < cd+in.pts; n++ {
					s.firstD[n] = int32(idx)
				}
				cd += in.pts
			} else {
				for n := cp; n < cp+in.pts; n++ {
					s.firstP[n] = int32(idx)
				}
				cp += in.pts
			}
		}
		if s.gD > maxGD {
			maxGD = s.gD
		}
		if s.gP > maxGP {
			maxGP = s.gP
		}
		seqs = append(seqs, s)
	}

	type gainW struct {
		gD, gP int
		w      float64
	}
	var hist []gainW
	{
		m := map[[2]int]float64{}
		for _, s := range seqs {
			m[[2]int{s.gD, s.gP}] += s.w
		}
		for k, w := range m {
			hist = append(hist, gainW{k[0], k[1], w})
		}
	}

	// --- DP ----------------------------------------------------------------------
	var W [target][target]float64
	for total := 2 * (target - 1); total >= 0; total-- {
		for sD := 0; sD < target; sD++ {
			sP := total - sD
			if sP < 0 || sP >= target {
				continue
			}
			needD, needP := target-sD, target-sP
			var p float64
			if needD <= maxGD && needP <= maxGP {
				// Both in reach: crossing order inside the deal decides.
				for _, s := range seqs {
					fd, fp := int32(-1), int32(-1)
					if needD <= s.gD {
						fd = s.firstD[needD-1]
					}
					if needP <= s.gP {
						fp = s.firstP[needP-1]
					}
					switch {
					case fd >= 0 && (fp < 0 || fd < fp):
						p += s.w
					case fp >= 0:
						// pone wins: contributes 0
					default:
						p += s.w * (1 - W[sP+s.gP][sD+s.gD])
					}
				}
			} else {
				// At most one side can cross: totals suffice.
				for _, h := range hist {
					switch {
					case h.gD >= needD:
						p += h.w
					case h.gP >= needP:
						// pone wins
					default:
						p += h.w * (1 - W[sP+h.gP][sD+h.gD])
					}
				}
			}
			W[sD][sP] = p
		}
	}
	fmt.Fprintf(os.Stderr, "  DP done (%.0fs); W[0][0] = %.4f (expect ≈ 0.561)\n",
		time.Since(start).Seconds(), W[0][0])

	// --- emit ----------------------------------------------------------------------
	var buf bytes.Buffer
	buf.WriteString("// Code generated by go generate (winprob_gen.go); DO NOT EDIT.\n\n")
	buf.WriteString("package eval\n\n")
	fmt.Fprintf(&buf, "// winProbDealer[sD][sP] = P(the player about to deal wins | dealer at sD,\n")
	fmt.Fprintf(&buf, "// pone at sP), from champion self-play over %d games (%d complete deals,\n", games, int(totalDeals))
	fmt.Fprintf(&buf, "// %d distinct counting-order sequences) and exact DP over the observed\n", len(seqs))
	fmt.Fprintf(&buf, "// sequences. See winprob_gen.go.\n")
	buf.WriteString("var winProbDealer = [121][121]float32{\n")
	for sD := 0; sD < target; sD++ {
		buf.WriteString("\t{")
		for sP := 0; sP < target; sP++ {
			if sP > 0 {
				buf.WriteString(", ")
			}
			fmt.Fprintf(&buf, "%g", math.Round(W[sD][sP]*10000)/10000)
		}
		buf.WriteString("},\n")
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(&buf, "// Per-deal outcome marginals from the same self-play run, for the win-aware\n")
	fmt.Fprintf(&buf, "// discard objective: show-score distributions by role, the joint pegging\n")
	fmt.Fprintf(&buf, "// distribution (dealer, pone), and the his-heels probability.\n")
	buf.WriteString("var oppHandDist = [2][30]float32{\n")
	for role := 0; role < 2; role++ {
		buf.WriteString("\t{")
		for i, v := range handDist[role] {
			if i > 0 {
				buf.WriteString(", ")
			}
			fmt.Fprintf(&buf, "%g", math.Round(v/totalDeals*10000)/10000)
		}
		buf.WriteString("},\n")
	}
	buf.WriteString("}\n\n")

	buf.WriteString("var pegJointDist = []pegJoint{\n")
	for k, w := range pegJoint {
		fmt.Fprintf(&buf, "\t{%d, %d, %g},\n", k[0], k[1], math.Round(w/totalDeals*1e6)/1e6)
	}
	buf.WriteString("}\n\n")

	fmt.Fprintf(&buf, "var heelsProb = float32(%g)\n", math.Round(heelsDeals/totalDeals*10000)/10000)

	src, err := format.Source(buf.Bytes())
	if err != nil {
		fmt.Fprintf(os.Stderr, "gofmt: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", outPath)
}
