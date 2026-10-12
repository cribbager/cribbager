package vectors

import (
	"fmt"
	"math/rand"

	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// Seats are 0 and 1 in the vectors; seat 0 is the Swift engine's Player.one.

// GamePlay is one card played, by a seat, during pegging.
type GamePlay struct {
	Seat int    `json:"seat"`
	Card string `json:"card"`
}

// GameEvent is the engine-neutral normalisation of what happened. Types:
//
//	heels  seat=dealer, points=2   (jack starter)
//	play   seat, card, points      (points scored by that play)
//	go     seat, points=1          (go or last card)
//	hand   seat, points            (a hand counted at the show)
//	crib   seat=dealer, points     (the crib counted at the show)
//	won    seat                    (game over; nothing after it is counted)
//
// Passes and count resets are deliberately not part of the contract (the two
// engines model them differently); their effects show up in who plays and in
// the go points.
type GameEvent struct {
	Type   string `json:"type"`
	Seat   int    `json:"seat"`
	Card   string `json:"card,omitempty"`
	Points int    `json:"points,omitempty"`
}

// GameVector is one deal played from a scripted position: the cards, the
// scores going in, the discards, the starter and the order of plays, and the
// resulting events, final scores and winner (-1 if nobody won this hand).
type GameVector struct {
	Name     string      `json:"name,omitempty"`
	Dealer   int         `json:"dealer"`
	Scores   [2]int      `json:"scores"`
	Hands    [2][]string `json:"hands"`
	Discards [2][]string `json:"discards"`
	Starter  string      `json:"starter"`
	Plays    []GamePlay  `json:"plays"`
	Events   []GameEvent `json:"events"`
	Final    [2]int      `json:"finalScores"`
	Winner   int         `json:"winner"`
}

type scenario struct {
	dealer   int
	scores   [2]int
	hands    [2][]cribbage.Card
	discards [2][]cribbage.Card
	starter  cribbage.Card
}

// playScenario runs a scenario to the end of the hand (or the game). If plays is
// nil it chooses random legal plays with rng and records them; otherwise it
// replays the given plays.
func playScenario(sc scenario, plays []GamePlay, rng *rand.Rand) GameVector {
	dealer := game.Seat(sc.dealer)
	pone := dealer ^ 1

	// Deck order: pone gets the even positions, dealer the odd, 12 is the starter.
	var d []cribbage.Card
	for i := 0; i < 6; i++ {
		d = append(d, sc.hands[pone][i], sc.hands[dealer][i])
	}
	d = append(d, sc.starter)
	seen := map[cribbage.Card]bool{}
	for _, c := range d {
		if seen[c] {
			panic("vectors: duplicate card in scenario")
		}
		seen[c] = true
	}
	for _, c := range cribbage.Deck() {
		if !seen[c] {
			d = append(d, c)
		}
	}
	g := game.New(game.Options{
		// The second deck only feeds the next deal, which the vectors ignore.
		Deck:  game.NewScriptedDeck(d, cribbage.Deck()),
		Start: &game.Start{Scores: sc.scores, Dealer: dealer},
	})

	v := GameVector{Dealer: sc.dealer, Scores: sc.scores, Starter: enc(sc.starter), Winner: -1}
	for s := 0; s < 2; s++ {
		v.Hands[s] = encAll(sc.hands[s])
		v.Discards[s] = encAll(sc.discards[s])
	}

	var evs []game.Event
	apply := func(seat game.Seat, cmd game.Command) {
		out, err := g.Apply(seat, cmd)
		if err != nil {
			panic(fmt.Sprintf("vectors: scripted game rejected %v: %v", cmd, err))
		}
		evs = append(evs, out...)
	}
	for _, s := range []game.Seat{pone, dealer} {
		apply(s, game.Discard{Cards: [2]cribbage.Card{sc.discards[s][0], sc.discards[s][1]}})
	}

	if plays == nil {
		for g.View(0).Phase == game.PhasePlay {
			turn := *g.View(0).ToPlay
			legal := g.View(turn).LegalPlays
			c := legal[rng.Intn(len(legal))]
			plays = append(plays, GamePlay{Seat: int(turn), Card: enc(c)})
			apply(turn, game.Play{Card: c})
		}
	} else {
		var applied []GamePlay
		for _, p := range plays {
			if g.View(0).Phase != game.PhasePlay {
				break // game ended early (a different starting score); drop the rest
			}
			apply(game.Seat(p.Seat), game.Play{Card: card(p.Card)})
			applied = append(applied, p)
		}
		plays = applied
	}
	v.Plays = plays
	if v.Plays == nil {
		v.Plays = []GamePlay{}
	}

	v.Events = []GameEvent{}
recording:
	for _, e := range evs {
		switch e := e.(type) {
		case game.HandDealt:
			break recording // the next hand: stop here
		case game.StarterCut:
			if e.Heels > 0 {
				v.Events = append(v.Events, GameEvent{Type: "heels", Seat: sc.dealer, Points: e.Heels})
			}
		case game.CardPlayed:
			v.Events = append(v.Events, GameEvent{Type: "play", Seat: int(e.Seat), Card: enc(e.Card), Points: e.Score.Total})
		case game.GoAwarded:
			v.Events = append(v.Events, GameEvent{Type: "go", Seat: int(e.Seat), Points: e.Points})
		case game.HandShown:
			v.Events = append(v.Events, GameEvent{Type: "hand", Seat: int(e.Seat), Points: e.Score.Total})
		case game.CribShown:
			v.Events = append(v.Events, GameEvent{Type: "crib", Seat: sc.dealer, Points: e.Score.Total})
		case game.GameWon:
			v.Events = append(v.Events, GameEvent{Type: "won", Seat: int(e.Seat)})
		}
	}

	// Final scores: replay the points, clamping at 121 like the engine does.
	v.Final = sc.scores
	for _, e := range v.Events {
		switch e.Type {
		case "heels", "play", "go", "hand", "crib":
			v.Final[e.Seat] = min(121, v.Final[e.Seat]+e.Points)
		case "won":
			v.Winner = e.Seat
		}
	}
	if g.Scores() != v.Final {
		// Only the next-hand HandDealt can follow, so the engine's own score
		// must match the replayed one; if not, the normalisation is wrong.
		panic(fmt.Sprintf("vectors: replayed scores %v != engine scores %v", v.Final, g.Scores()))
	}
	return v
}

// randomScenario deals a random hand. forceStarter, if non-nil, becomes the starter.
func randomScenario(rng *rand.Rand, forceStarter *cribbage.Card) scenario {
	deck := cribbage.Deck()
	rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	if forceStarter != nil {
		for i, c := range deck {
			if c == *forceStarter {
				deck[i], deck[12] = deck[12], deck[i]
			}
		}
	}
	var sc scenario
	sc.dealer = rng.Intn(2)
	sc.hands[0], sc.hands[1] = deck[0:6:6], deck[6:12:12]
	sc.starter = deck[12]
	for s := 0; s < 2; s++ {
		perm := rng.Perm(6)
		sc.discards[s] = []cribbage.Card{sc.hands[s][perm[0]], sc.hands[s][perm[1]]}
	}
	if rng.Intn(3) > 0 {
		sc.scores = [2]int{rng.Intn(121), rng.Intn(121)}
		if rng.Intn(2) == 0 { // late-game positions are where wins happen
			sc.scores = [2]int{85 + rng.Intn(36), 85 + rng.Intn(36)}
		}
	}
	return sc
}

type winSpec struct {
	name    string
	pick    func(v GameVector) int // index of the event to win on, or -1
	min     int                    // minimum points the event must carry
	oneOnly bool                   // the event only ever scores 1 (go point)
}

func nth(typ string, n int, ok func(v GameVector, e GameEvent) bool) func(GameVector) int {
	return func(v GameVector) int {
		seen := 0
		for i, e := range v.Events {
			if e.Type == typ && (ok == nil || ok(v, e)) {
				if seen == n {
					return i
				}
				seen++
			}
		}
		return -1
	}
}

func gameVectors(seed int64) []GameVector {
	rng := rand.New(rand.NewSource(seed))
	var out []GameVector

	// Hand-picked situations, built by searching seeded deals for the event we
	// want and then setting the starting score so the game is won exactly on
	// that event (or one point past it, to exercise the clamp at 121).
	scoring := func(v GameVector, e GameEvent) bool { return e.Points > 0 }
	isPone := func(v GameVector, e GameEvent) bool { return e.Seat != v.Dealer }
	isDealer := func(v GameVector, e GameEvent) bool { return e.Seat == v.Dealer }
	specs := []winSpec{
		{"pegging out on a play", nth("play", 3, scoring), 1, false},
		{"pegging out on the last scoring play", func(v GameVector) int {
			last := -1
			for i, e := range v.Events {
				if e.Type == "play" && e.Points > 0 {
					last = i
				}
			}
			return last
		}, 1, false},
		{"pegging out on a go point", nth("go", 0, nil), 1, true},
		{"winning on the pone's hand (dealer's hand and crib never count)", nth("hand", 0, isPone), 2, false},
		{"winning on the dealer's hand (crib never counts)", nth("hand", 0, isDealer), 2, false},
		{"winning on the crib", nth("crib", 0, nil), 2, false},
	}
	for _, w := range specs {
		for _, overshoot := range []bool{false, true} {
			if overshoot && w.oneOnly {
				continue // a 1-point event cannot overshoot
			}
			found := 0
			for tries := 0; tries < 50000 && found < 2; tries++ {
				sc := randomScenario(rng, nil)
				sc.scores = [2]int{}
				base := playScenario(sc, nil, rng)
				idx := w.pick(base)
				if idx < 0 {
					continue
				}
				e := base.Events[idx]
				if e.Points < w.min || (overshoot && e.Points < 2) {
					continue
				}
				cum := 0
				for _, p := range base.Events[:idx+1] {
					if p.Seat == e.Seat {
						cum += p.Points
					}
				}
				start := 121 - cum
				if overshoot {
					start++
				}
				if start < 0 || start > 120 {
					continue
				}
				sc.scores[e.Seat] = start
				v := playScenario(sc, base.Plays, nil)
				// The game must end on exactly this event, won by this seat.
				if v.Winner != e.Seat || len(v.Events) != idx+2 || v.Events[idx].Type != e.Type {
					continue
				}
				if overshoot {
					v.Name = w.name + ", overshooting 121 (score stops at 121)"
				} else {
					v.Name = w.name + ", landing exactly on 121"
				}
				out = append(out, v)
				found++
			}
			if found == 0 {
				panic("vectors: could not construct game vector: " + w.name)
			}
		}
	}

	// His heels: a jack starter pegs 2 for the dealer, and can win the game.
	for _, spec := range []struct {
		name  string
		score int
		suit  cribbage.Suit
	}{
		{"his heels: jack starter scores 2 for the dealer", 0, cribbage.Hearts},
		{"his heels wins the game from 119 (exactly 121)", 119, cribbage.Spades},
		{"his heels wins the game from 120 (score stops at 121)", 120, cribbage.Clubs},
		{"his heels from 117 lands on 119 and play continues", 117, cribbage.Diamonds},
	} {
		j := cribbage.Card{Rank: cribbage.Jack, Suit: spec.suit}
		for {
			sc := randomScenario(rng, &j)
			sc.scores = [2]int{}
			sc.scores[sc.dealer] = spec.score
			v := playScenario(sc, nil, rng)
			if spec.score == 117 && v.Winner >= 0 {
				continue // want a quiet continuation, not a later win
			}
			v.Name = spec.name
			out = append(out, v)
			break
		}
	}

	// Full hands from 0-0, then seeded random positions.
	for i := 0; i < 8; i++ {
		sc := randomScenario(rng, nil)
		sc.scores = [2]int{}
		v := playScenario(sc, nil, rng)
		v.Name = "full hand from 0-0"
		out = append(out, v)
	}
	for i := 0; i < 100; i++ {
		out = append(out, playScenario(randomScenario(rng, nil), nil, rng))
	}
	return out
}
