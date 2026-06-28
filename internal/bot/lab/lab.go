// Package lab holds challenger bots under active development and is the place to
// pit them against the shipped champion (internal/bot). It is imported only by
// its own tests — never by the client or server — so a challenger can never
// become the shipped opponent.
//
// The workflow is a one-way ratchet: build a challenger here, beat the champion
// in a duplicate-deal comparison (bot.Compare, run from the go test in
// challenger_test.go), then fold the winning change into internal/bot's champion
// and DELETE the challenger. Losers are deleted too. There is only ever one
// shipped bot; git history is the archive of what was tried.
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
