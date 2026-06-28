package main

import (
	"strings"

	"github.com/cribbager/cribbager/internal/cribbage"
)

// normalizeCardText makes human-typed card text canonical: upper-cased, with
// "10" accepted as an alias for the ten's rank letter "T" (so "10C" == "TC").
func normalizeCardText(s string) string {
	t := strings.ToUpper(strings.TrimSpace(s))
	if strings.HasPrefix(t, "10") {
		t = "T" + t[2:]
	}
	return t
}

// parseCardArg parses one human-typed card, tolerating "10" and case via
// normalizeCardText. The core cribbage.ParseCard stays strict two-char notation
// for the golden fixtures and the wire protocol.
func parseCardArg(s string) (cribbage.Card, error) {
	return cribbage.ParseCard(normalizeCardText(s))
}
