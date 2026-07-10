// External test package: the fidelity test drives real bots (internal/bot),
// which imports game, so this cannot live inside package game without a cycle.
package game_test

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/cribbager/cribbager/internal/bot"
	"github.com/cribbager/cribbager/internal/cribbage"
	"github.com/cribbager/cribbager/internal/game"
)

// liveDecision is one pegging choice captured DURING live play: the view the
// bot actually decided from and the card it chose, recorded before Apply. This
// is the ground truth ReconstructPlays must reproduce from the event log alone.
type liveDecision struct {
	deal   int
	seat   game.Seat
	view   game.PlayerView
	played cribbage.Card
}

// driveGame plays a complete game (a is seat 0, b is seat 1), capturing every
// play decision live at the moment it was made. The loop mirrors bot.playOn.
func driveGame(t *testing.T, a, b bot.Bot, deck game.DeckSource) (*game.Game, []liveDecision) {
	t.Helper()
	g := game.New(game.Options{Deck: deck})
	bots := [2]bot.Bot{a, b}
	var live []liveDecision
	for {
		if _, over := g.Winner(); over {
			return g, live
		}
		v := g.View(game.Seat0)
		switch v.Phase {
		case game.PhaseDiscard:
			for s := game.Seat(0); s < 2; s++ {
				vs := g.View(s)
				if len(vs.YourHand) != 6 {
					continue
				}
				if _, err := g.Apply(s, game.Discard{Cards: bots[s].Discard(vs)}); err != nil {
					t.Fatalf("%s discard: %v", bots[s].Name(), err)
				}
			}
		case game.PhasePlay:
			seat := *v.ToPlay
			vs := g.View(seat)
			card := bots[seat].Play(vs)
			live = append(live, liveDecision{deal: dealsSoFar(g) - 1, seat: seat, view: vs, played: card})
			if _, err := g.Apply(seat, game.Play{Card: card}); err != nil {
				t.Fatalf("%s play: %v", bots[seat].Name(), err)
			}
		default:
			t.Fatalf("unexpected phase %v", v.Phase)
		}
	}
}

// dealsSoFar counts HandDealt events in the live log — the 0-based deal index
// of the current hand is dealsSoFar-1.
func dealsSoFar(g *game.Game) int {
	n := 0
	for _, e := range g.Events() {
		if _, ok := e.(game.HandDealt); ok {
			n++
		}
	}
	return n
}

// endsMidDeal reports whether the game was won during pegging (a peg-out): the
// event before GameWon is a play or a go point, and the deal's show never ran.
func endsMidDeal(events []game.Event) bool {
	for i, e := range events {
		if _, ok := e.(game.GameWon); ok && i > 0 {
			switch events[i-1].(type) {
			case game.CardPlayed, game.GoAwarded:
				return true
			}
			return false
		}
	}
	return false
}

func countPlays(events []game.Event) int {
	n := 0
	for _, e := range events {
		if _, ok := e.(game.CardPlayed); ok {
			n++
		}
	}
	return n
}

// TestReconstructPlaysFidelity is the core guarantee: play complete games with
// real bots, capturing every (deal, seat, view, card) live at each play
// decision, then reconstruct the decisions from the finished game's event log
// alone and require an EXACT match — every PlayerView field an analyzer could
// consume (hand, pile, count, played cards, discards, starter, scores, dealer,
// legal plays, turn, version), via DeepEqual on the whole decision.
//
// The corpus mixes seeds and bot types (random for varied, mistake-rich play;
// the deterministic production bots for realistic play) and must contain both
// game endings: mid-deal (someone pegs out during pegging, so the last deal has
// no show) and at the show.
func TestReconstructPlaysFidelity(t *testing.T) {
	type pairing struct {
		name string
		a, b bot.Bot
	}
	sawPegOut, sawShowEnd := false, false

	for seed := int64(1); seed <= 25; seed++ {
		pairs := []pairing{
			{"random-vs-random",
				bot.NewRandom(rand.New(rand.NewSource(seed))),
				bot.NewRandom(rand.New(rand.NewSource(seed + 1000)))},
		}
		// The deterministic production bots are slower; a few seeds suffice.
		if seed <= 3 {
			ml, err := bot.New(bot.DefaultName, rand.New(rand.NewSource(seed)))
			if err != nil {
				t.Fatalf("build %s: %v", bot.DefaultName, err)
			}
			pairs = append(pairs, pairing{"ml-vs-champion", ml, bot.Champion()})
		}

		for _, p := range pairs {
			g, live := driveGame(t, p.a, p.b, game.NewSeededDeck(seed))
			events := g.Events()

			got, err := game.ReconstructPlays(events)
			if err != nil {
				t.Fatalf("seed %d %s: ReconstructPlays: %v", seed, p.name, err)
			}
			if len(got) != len(live) {
				t.Fatalf("seed %d %s: reconstructed %d decisions, captured %d live",
					seed, p.name, len(got), len(live))
			}
			if n := countPlays(events); len(got) != n {
				t.Fatalf("seed %d %s: %d decisions for %d CardPlayed events",
					seed, p.name, len(got), n)
			}
			for i, l := range live {
				want := game.PlayDecision{Deal: l.deal, Seat: l.seat, View: l.view, Played: l.played}
				if !reflect.DeepEqual(got[i], want) {
					t.Fatalf("seed %d %s: decision %d differs from live capture:\n got  %+v\n want %+v",
						seed, p.name, i, got[i], want)
				}
			}

			if endsMidDeal(events) {
				sawPegOut = true
			} else {
				sawShowEnd = true
			}
		}
	}

	// The corpus must exercise both endings, or the mid-deal path is untested.
	if !sawPegOut {
		t.Error("no game in the corpus ended mid-deal (peg-out); widen the seed range")
	}
	if !sawShowEnd {
		t.Error("no game in the corpus ended at the show; widen the seed range")
	}
}

// TestReconstructPlaysPrefix checks that a prefix of a live game's log (an
// unfinished game) reconstructs cleanly too — reconstruction does not require
// GameWon, only a legal history.
func TestReconstructPlaysPrefix(t *testing.T) {
	g, live := driveGame(t,
		bot.NewRandom(rand.New(rand.NewSource(9))),
		bot.NewRandom(rand.New(rand.NewSource(1009))),
		game.NewSeededDeck(9))
	events := g.Events()

	// Cut the log just after its 3rd CardPlayed: the first 3 decisions must
	// reconstruct identically, and later ones must not appear.
	plays := 0
	cut := 0
	for i, e := range events {
		if _, ok := e.(game.CardPlayed); ok {
			plays++
			if plays == 3 {
				cut = i + 1
				break
			}
		}
	}
	if cut == 0 {
		t.Fatal("game had fewer than 3 plays")
	}

	got, err := game.ReconstructPlays(events[:cut])
	if err != nil {
		t.Fatalf("ReconstructPlays(prefix): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("prefix reconstructed %d decisions, want 3", len(got))
	}
	for i := 0; i < 3; i++ {
		want := game.PlayDecision{Deal: live[i].deal, Seat: live[i].seat, View: live[i].view, Played: live[i].played}
		if !reflect.DeepEqual(got[i], want) {
			t.Fatalf("prefix decision %d differs from live capture:\n got  %+v\n want %+v", i, got[i], want)
		}
	}
}

// playPhaseLog drives a fresh seeded game to the start of the play phase and
// returns its log plus both seats' views (for building corrupt continuations).
func playPhaseLog(t *testing.T) (events []game.Event, toPlay game.Seat, views [2]game.PlayerView) {
	t.Helper()
	g := game.New(game.Options{Deck: game.NewSeededDeck(1)})
	c := bot.Champion()
	for s := game.Seat(0); s < 2; s++ {
		if _, err := g.Apply(s, game.Discard{Cards: c.Discard(g.View(s))}); err != nil {
			t.Fatalf("discard: %v", err)
		}
	}
	v := g.View(game.Seat0)
	if v.Phase != game.PhasePlay || v.ToPlay == nil {
		t.Fatalf("expected play phase, got %v", v.Phase)
	}
	return g.Events(), *v.ToPlay, [2]game.PlayerView{g.View(game.Seat0), g.View(game.Seat1)}
}

// TestReconstructPlaysRejectsCorruptLogs: a log that is not a legal game
// history must produce an error, never a fabricated decision.
func TestReconstructPlaysRejectsCorruptLogs(t *testing.T) {
	events, toPlay, views := playPhaseLog(t)
	offTurn := toPlay ^ 1

	t.Run("empty log", func(t *testing.T) {
		got, err := game.ReconstructPlays(nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v; want empty, nil", got, err)
		}
	})

	t.Run("play before any deal", func(t *testing.T) {
		log := []game.Event{game.CardPlayed{Seat: game.Seat0, Card: views[0].YourHand[0]}}
		if _, err := game.ReconstructPlays(log); err == nil {
			t.Fatal("want error for CardPlayed before HandDealt")
		}
	})

	t.Run("play in discard phase", func(t *testing.T) {
		// Truncate before the second discard: still in the discard phase.
		var cutAt int
		for i, e := range events {
			if _, ok := e.(game.Discarded); ok {
				cutAt = i // keep everything before the first Discarded
				break
			}
		}
		log := append(append([]game.Event(nil), events[:cutAt]...),
			game.CardPlayed{Seat: toPlay, Card: views[toPlay].YourHand[0]})
		if _, err := game.ReconstructPlays(log); err == nil {
			t.Fatal("want error for CardPlayed during the discard phase")
		}
	})

	t.Run("play out of turn", func(t *testing.T) {
		log := append(append([]game.Event(nil), events...),
			game.CardPlayed{Seat: offTurn, Card: views[offTurn].YourHand[0]})
		if _, err := game.ReconstructPlays(log); err == nil {
			t.Fatal("want error for out-of-turn CardPlayed")
		}
	})

	t.Run("play a card not in hand", func(t *testing.T) {
		// The opponent's card is guaranteed not to be in the on-turn hand.
		log := append(append([]game.Event(nil), events...),
			game.CardPlayed{Seat: toPlay, Card: views[offTurn].YourHand[0]})
		if _, err := game.ReconstructPlays(log); err == nil {
			t.Fatal("want error for CardPlayed with a card not in hand")
		}
	})
}

// TestReconstructPlaysRejectsOver31 corrupts a real game at a moment when the
// player held a card that would bust the count: recording that card as played
// must be rejected (in-hand but illegal — the count check specifically).
func TestReconstructPlaysRejectsOver31(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		g := game.New(game.Options{Deck: game.NewSeededDeck(seed)})
		bots := [2]bot.Bot{
			bot.NewRandom(rand.New(rand.NewSource(seed))),
			bot.NewRandom(rand.New(rand.NewSource(seed + 1000))),
		}
		for {
			if _, over := g.Winner(); over {
				break
			}
			v := g.View(game.Seat0)
			switch v.Phase {
			case game.PhaseDiscard:
				for s := game.Seat(0); s < 2; s++ {
					if vs := g.View(s); len(vs.YourHand) == 6 {
						if _, err := g.Apply(s, game.Discard{Cards: bots[s].Discard(vs)}); err != nil {
							t.Fatal(err)
						}
					}
				}
			case game.PhasePlay:
				seat := *v.ToPlay
				vs := g.View(seat)
				// Look for a held card that is NOT legal (would exceed 31).
				for _, c := range vs.YourHand {
					legal := false
					for _, lp := range vs.LegalPlays {
						if lp == c {
							legal = true
							break
						}
					}
					if !legal {
						log := append(g.Events(), game.CardPlayed{Seat: seat, Card: c})
						if _, err := game.ReconstructPlays(log); err == nil {
							t.Fatal("want error for a play that takes the count past 31")
						}
						return // found and verified the scenario
					}
				}
				if _, err := g.Apply(seat, game.Play{Card: bots[seat].Play(vs)}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Fatal("no over-31 hold found in 50 seeded games; widen the search")
}
