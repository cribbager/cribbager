// Package lab holds bots under active development and is the place to pit them
// against the production bots (internal/bot). It is imported only by its own
// tests — never by the client or server — so a challenger can never be seated in
// a real game until it is promoted into internal/bot.
//
// Challengers may be long-lived: an experiment (e.g. an ML bot trained over
// weeks) can live here, registered alongside others, while it is developed and
// measured against a rival with bot.Compare (run from the go test in
// challenger_test.go). When one is ready it is PROMOTED into internal/bot — which
// may mean replacing the champion, or shipping as a new named production bot that
// coexists with the champion (now the default opponent, not the only one).
// Abandoned experiments are deleted; git history is the archive of what was tried.
package lab

import (
	"sort"

	"github.com/cribbager/cribbager/internal/bot"
)

// registry maps a challenger's name to its constructor. Challengers add
// themselves from an init() in their own file (see candidate.go).
var registry = map[string]func() bot.Bot{}

// Register adds a challenger under name. It panics on a duplicate name so two
// challengers can't silently collide.
func Register(name string, make func() bot.Bot) {
	if _, dup := registry[name]; dup {
		panic("lab: duplicate challenger " + name)
	}
	registry[name] = make
}

// New builds a challenger by name; ok is false if there is no such challenger.
func New(name string) (b bot.Bot, ok bool) {
	make, ok := registry[name]
	if !ok {
		return nil, false
	}
	return make(), true
}

// Names lists the registered challengers, sorted.
func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
