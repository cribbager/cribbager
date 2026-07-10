package game

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/scoring/pegging"
)

// --- a random legal-move driver ----------------------------------------------

// playFullGame plays a complete game with random legal moves. When check is set,
// it verifies card conservation and visibility after every command.
func playFullGame(t *testing.T, seed int64, check bool) *Game {
	t.Helper()
	g := New(Options{Deck: NewSeededDeck(seed)})
	driveRandom(t, g, seed, check)
	return g
}

// driveRandom plays an in-progress game to completion with random legal moves.
func driveRandom(t *testing.T, g *Game, seed int64, check bool) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))

	steps := 0
	for {
		if _, ok := g.Winner(); ok {
			break
		}
		steps++
		if steps > 100_000 {
			t.Fatalf("seed %d: game did not terminate", seed)
		}

		switch g.phase {
		case PhaseDiscard:
			s := Seat0
			if g.discarded[Seat0] {
				s = Seat1
			}
			h := g.hands[s]
			i := rng.Intn(len(h))
			j := rng.Intn(len(h) - 1)
			if j >= i {
				j++
			}
			if _, err := g.Apply(s, Discard{Cards: [2]cribbage.Card{h[i], h[j]}}); err != nil {
				t.Fatalf("seed %d: discard error: %v", seed, err)
			}
		case PhasePlay:
			s := g.turn()
			var legal []cribbage.Card
			for _, c := range g.hands[s] {
				if g.count+c.Rank.PipValue() <= 31 {
					legal = append(legal, c)
				}
			}
			if len(legal) == 0 {
				t.Fatalf("seed %d: turn() returned %v with no legal play", seed, s)
			}
			card := legal[rng.Intn(len(legal))]
			if _, err := g.Apply(s, Play{Card: card}); err != nil {
				t.Fatalf("seed %d: play error: %v", seed, err)
			}
		default:
			t.Fatalf("seed %d: unexpected phase %v", seed, g.phase)
		}

		if check {
			conserve(t, g, seed)
			checkVisibility(t, g, seed)
		}
	}
}

// --- invariants ---------------------------------------------------------------

// conserve checks that the current hand's 52 cards are all present exactly once.
func conserve(t *testing.T, g *Game, seed int64) {
	t.Helper()
	seen := map[cribbage.Card]int{}
	add := func(cs []cribbage.Card) {
		for _, c := range cs {
			seen[c]++
		}
	}
	add(g.hands[Seat0])
	add(g.hands[Seat1])
	add(g.played[Seat0])
	add(g.played[Seat1])
	add(g.crib)
	add(g.rest)
	if g.hasStarter {
		seen[g.starter]++
	}
	if len(seen) != 52 {
		t.Fatalf("seed %d: %d distinct cards, want 52", seed, len(seen))
	}
	for c, n := range seen {
		if n != 1 {
			t.Fatalf("seed %d: card %s appears %d times", seed, c, n)
		}
	}
}

// checkVisibility ensures a seat's view never contains a hidden card.
func checkVisibility(t *testing.T, g *Game, seed int64) {
	t.Helper()
	for s := Seat(0); s < 2; s++ {
		forbidden := map[cribbage.Card]bool{}
		for _, c := range g.hands[other(s)] {
			forbidden[c] = true
		}
		// The crib is hidden EXCEPT a seat's own throw, which it remembers.
		for _, c := range g.crib {
			forbidden[c] = true
		}
		if g.discarded[s] {
			for _, c := range g.cribThrown[s] {
				delete(forbidden, c)
			}
		}
		for _, c := range g.rest {
			forbidden[c] = true
		}
		v := g.View(s)
		exposed := append([]cribbage.Card{}, v.YourHand...)
		exposed = append(exposed, v.YourDiscards...)
		exposed = append(exposed, v.Pile...)
		exposed = append(exposed, v.LegalPlays...)
		for _, c := range exposed {
			if forbidden[c] {
				t.Fatalf("seed %d: view for %v leaked hidden card %s", seed, s, c)
			}
		}
		if v.OpponentCards != len(g.hands[other(s)]) {
			t.Fatalf("seed %d: wrong opponent card count", seed)
		}
		if g.discarded[s] {
			if len(v.YourDiscards) != 2 || v.YourDiscards[0] != g.cribThrown[s][0] || v.YourDiscards[1] != g.cribThrown[s][1] {
				t.Fatalf("seed %d: view for %v has wrong YourDiscards %v", seed, s, v.YourDiscards)
			}
		} else if len(v.YourDiscards) != 0 {
			t.Fatalf("seed %d: view for %v has YourDiscards before discarding", seed, s)
		}
	}
}

// reconcile checks that the events' points, applied under the board rule —
// events carry the full count, the score stops at the target (addPoints) —
// equal the final scores.
func reconcile(t *testing.T, g *Game, seed int64) {
	t.Helper()
	var pts [2]int
	dealer := Seat0
	award := func(seat Seat, p int) {
		pts[seat] += p
		if pts[seat] > g.target {
			pts[seat] = g.target
		}
	}
	for _, e := range g.Events() {
		switch e := e.(type) {
		case CutForDeal:
			dealer = e.Dealer
		case Handicap:
			pts = e.Scores
		case HandDealt:
			dealer = e.Dealer
		case StarterCut:
			award(dealer, e.Heels)
		case CardPlayed:
			award(e.Seat, e.Score.Total)
		case GoAwarded:
			award(e.Seat, e.Points)
		case HandShown:
			award(e.Seat, e.Score.Total)
		case CribShown:
			award(dealer, e.Score.Total)
		}
	}
	if pts != g.scores {
		t.Fatalf("seed %d: reconciled points %v != scores %v", seed, pts, g.scores)
	}
}

// foldEqual replays the event log into a fresh game and checks the foldable
// state matches — i.e. the live state really is the fold of the log. The fold
// must run under the same target: score application caps at it (addPoints).
func foldEqual(t *testing.T, g *Game, seed int64) {
	t.Helper()
	r := &Game{winner: -1, target: g.target}
	for _, e := range g.Events() {
		r.reduce(e)
	}
	if !reflect.DeepEqual(r.snapshot(), g.snapshot()) {
		t.Fatalf("seed %d: fold of log != live state", seed)
	}
}

// snapshot captures the foldable state for comparison (excludes transient fields
// like the deck source, remaining deck, and version).
func (g *Game) snapshot() any {
	return struct {
		Dealer     Seat
		Scores     [2]int
		Phase      Phase
		Hands      [2][]cribbage.Card
		Played     [2][]cribbage.Card
		Crib       []cribbage.Card
		Starter    cribbage.Card
		HasStarter bool
		Discarded  [2]bool
		CribThrown [2][2]cribbage.Card
		Pile       []cribbage.Card
		Count      int
		Gone       [2]bool
		LastPlayer Seat
		Winner     int
	}{
		g.dealer, g.scores, g.phase, g.hands, g.played, g.crib, g.starter,
		g.hasStarter, g.discarded, g.cribThrown, g.pile, g.count, g.gone, g.lastPlayer, g.winner,
	}
}

// --- the independent play-flow oracle ----------------------------------------

type playRec struct {
	seat Seat
	card cribbage.Card
}

// referencePlayPoints independently re-derives the play-phase points (pegging
// combos via the proven scorer, plus go/last-card via its own turn/reset logic)
// for one completed hand, and verifies the engine's recorded play order is the
// legal turn order. A turn-order or go bug in the engine makes this diverge.
func referencePlayPoints(t *testing.T, seq []playRec, leader Seat, seed int64) [2]int {
	var rem [2][]cribbage.Card
	for _, p := range seq {
		rem[p.seat] = append(rem[p.seat], p.card)
	}

	var pts [2]int
	var pile []cribbage.Card
	count := 0
	idx := 0
	seriesLeader := leader

	canPlay := func(cards []cribbage.Card) bool {
		for _, c := range cards {
			if count+c.Rank.PipValue() <= 31 {
				return true
			}
		}
		return false
	}

	for idx < len(seq) {
		turn := seriesLeader
		last := -1
		ended31 := false
		for {
			if canPlay(rem[turn]) {
				rec := seq[idx]
				idx++
				if rec.seat != turn {
					t.Fatalf("seed %d: oracle expected %v to play, engine had %v", seed, turn, rec.seat)
				}
				if !contains(rem[turn], rec.card) || count+rec.card.Rank.PipValue() > 31 {
					t.Fatalf("seed %d: engine played absent/illegal card %s", seed, rec.card)
				}
				r, _ := pegging.Score(pile, rec.card)
				pts[turn] += r.Total
				pile = append(pile, rec.card)
				count += rec.card.Rank.PipValue()
				rem[turn] = without(rem[turn], rec.card)
				last = int(turn)
				if count == 31 {
					ended31 = true
					break
				}
				turn = other(turn)
			} else if !canPlay(rem[other(turn)]) {
				break // both stuck
			} else {
				turn = other(turn)
			}
		}
		if !ended31 && last >= 0 {
			pts[last]++ // go / last card
		}
		if len(rem[Seat0]) == 0 && len(rem[Seat1]) == 0 {
			break
		}
		seriesLeader = other(Seat(last))
		if len(rem[seriesLeader]) == 0 {
			seriesLeader = Seat(last)
		}
		pile = nil
		count = 0
	}
	return pts
}

// checkPlayOracle segments the log into hands and runs the oracle on every hand
// that completed its play phase (i.e. reached the show).
func checkPlayOracle(t *testing.T, g *Game, seed int64) {
	t.Helper()
	type handRec struct {
		dealer    Seat
		seq       []playRec
		enginePts [2]int
		shown     bool
	}
	var hands []handRec
	cur := -1

	for _, e := range g.Events() {
		switch e := e.(type) {
		case HandDealt:
			hands = append(hands, handRec{dealer: e.Dealer})
			cur = len(hands) - 1
		case CardPlayed:
			hands[cur].seq = append(hands[cur].seq, playRec{e.Seat, e.Card})
			hands[cur].enginePts[e.Seat] += e.Score.Total
		case GoAwarded:
			hands[cur].enginePts[e.Seat] += e.Points
		case HandShown:
			hands[cur].shown = true
		}
	}

	for _, h := range hands {
		if !h.shown {
			continue // play interrupted by a win; covered by reconciliation
		}
		want := referencePlayPoints(t, h.seq, other(h.dealer), seed)
		if want != h.enginePts {
			t.Fatalf("seed %d: play points %v != oracle %v", seed, h.enginePts, want)
		}
	}
}

// --- the big simulation -------------------------------------------------------

func TestSimulation(t *testing.T) {
	const games = 4000
	for seed := int64(0); seed < games; seed++ {
		g := playFullGame(t, seed, true)

		w, ok := g.Winner()
		if !ok {
			t.Fatalf("seed %d: no winner", seed)
		}
		if g.scores[w] != g.target {
			t.Fatalf("seed %d: winner %v has %d, want exactly the target %d", seed, w, g.scores[w], g.target)
		}
		if loser := other(w); g.scores[loser] >= g.target {
			t.Fatalf("seed %d: both seats reached target", seed)
		}
		if g.phase != PhaseComplete {
			t.Fatalf("seed %d: winner but phase %v", seed, g.phase)
		}

		reconcile(t, g, seed)
		foldEqual(t, g, seed)
		checkPlayOracle(t, g, seed)
	}
}

// --- determinism --------------------------------------------------------------

func TestDeterministic(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		a := playFullGame(t, seed, false)
		b := playFullGame(t, seed, false)
		ea, eb := a.Events(), b.Events()
		if len(ea) != len(eb) {
			t.Fatalf("seed %d: event counts %d vs %d", seed, len(ea), len(eb))
		}
		for i := range ea {
			if !reflect.DeepEqual(ea[i], eb[i]) {
				t.Fatalf("seed %d: event %d differs", seed, i)
			}
		}
	}
}
