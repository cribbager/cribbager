package bot

import (
	"math/rand"
	"testing"

	"github.com/cribbager/cribbager/internal/game"
)

func mk(t *testing.T, name string, seed int64) Bot {
	t.Helper()
	b, err := New(name, rand.New(rand.NewSource(seed)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNewBuildsEveryProductionBot checks that every name Names() advertises is
// actually buildable by New (and reports that same name back), so the registry
// and the advertised list can't drift apart.
func TestNewBuildsEveryProductionBot(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("Names() is empty")
	}
	for _, name := range names {
		if !Valid(name) {
			t.Errorf("Valid(%q) = false for a name Names() lists", name)
		}
		b, err := New(name, rand.New(rand.NewSource(1)))
		if err != nil {
			t.Errorf("New(%q): %v", name, err)
			continue
		}
		if b.Name() != name {
			t.Errorf("New(%q).Name() = %q", name, b.Name())
		}
		if b.Version() == "" {
			t.Errorf("bot %q reports an empty version", name)
		}
	}
	// The default opponent must be one of the production bots.
	if !Valid(DefaultName) {
		t.Errorf("DefaultName %q is not a production bot", DefaultName)
	}
}

// TestNewUnknownBotRejected checks that an unrecognized name is a clean error
// (not a nil bot or a panic), so a bad request can be surfaced as a 4xx.
func TestNewUnknownBotRejected(t *testing.T) {
	b, err := New("nope", rand.New(rand.NewSource(1)))
	if err == nil {
		t.Fatal("New with an unknown name returned no error")
	}
	if b != nil {
		t.Errorf("New with an unknown name returned a non-nil bot: %v", b)
	}
	if Valid("nope") {
		t.Error("Valid reported an unknown name as valid")
	}
}

// TestLegalityAndTermination plays every bot pairing over many games. PlayGame
// returns an error if a bot ever produces a move the engine rejects, so a clean
// run proves bots always play legally — and every game must end with a winner at
// or past the target.
func TestLegalityAndTermination(t *testing.T) {
	pairs := [][2]string{
		{"random", "random"},
		{DefaultName, "random"},
		{DefaultName, DefaultName},
	}
	for _, p := range pairs {
		games := 60
		if p[0] == DefaultName || p[1] == DefaultName {
			games = 30
		}
		for seed := int64(0); seed < int64(games); seed++ {
			a := mk(t, p[0], seed)
			b := mk(t, p[1], seed+1000)
			res, err := PlayGame(a, b, game.NewSeededDeck(seed))
			if err != nil {
				t.Fatalf("%s vs %s seed %d: %v", p[0], p[1], seed, err)
			}
			if res.Scores[res.Winner] < 121 {
				t.Fatalf("%s vs %s seed %d: winner has %d", p[0], p[1], seed, res.Scores[res.Winner])
			}
		}
	}
}

func TestDeterministic(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		run := func() Result {
			a := mk(t, DefaultName, seed)
			b := mk(t, "random", seed+1)
			res, err := PlayGame(a, b, game.NewSeededDeck(seed))
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
		if run() != run() {
			t.Fatalf("seed %d: non-deterministic result", seed)
		}
	}
}

// winRate plays a series of A-vs-B games, swapping seats each game so neither
// bot gets a fixed-seat edge, and returns A's win fraction.
func winRate(t *testing.T, aName, bName string, games int, seed int64) float64 {
	t.Helper()
	a := mk(t, aName, seed)
	b := mk(t, bName, seed+1)
	aWins := 0
	for i := 0; i < games; i++ {
		deck := game.NewSeededDeck(seed + int64(i))
		var res Result
		var err error
		aSeat := game.Seat0
		if i%2 == 0 {
			res, err = PlayGame(a, b, deck)
		} else {
			res, err = PlayGame(b, a, deck)
			aSeat = game.Seat1
		}
		if err != nil {
			t.Fatal(err)
		}
		if res.Winner == aSeat {
			aWins++
		}
	}
	return float64(aWins) / float64(games)
}

// TestStrengthOrdering verifies the champion crushes the random baseline — a
// sanity check that the evaluator is actually working. Deterministic (seeded),
// so the threshold is conservative against the measured rate (~98%).
func TestStrengthOrdering(t *testing.T) {
	if lr := winRate(t, DefaultName, "random", 300, 1); lr < 0.90 {
		t.Errorf("champion vs random win rate %.3f, want >= 0.90", lr)
	}
}
