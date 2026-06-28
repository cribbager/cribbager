// Package cribbage holds the core domain types shared across the engine:
// scoring (hand and pegging), the game state machine, the bots, and notation.
// It deliberately knows nothing about scoring rules — those live in
// internal/scoring — so that every other package agrees on one vocabulary for
// cards.
package cribbage

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Suit identifies a card's suit. Suits are ordered Clubs, Diamonds, Hearts,
// Spades to match the single-letter codes (C, D, H, S) used by the golden
// scoring fixtures. The ordering is otherwise arbitrary — suit never
// affects a score except relationally (flush = all the same; nobs = same as the
// starter), so any consistent ordering works.
type Suit uint8

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
)

// Rank identifies a card's rank, Ace (1) through King (13). The value is the
// face rank, not the pip value used for fifteens — see PipValue. Ace is always
// low: there is no wraparound run (Queen-King-Ace is not a run).
type Rank uint8

const (
	Ace   Rank = 1
	Jack  Rank = 11
	Queen Rank = 12
	King  Rank = 13
)

// Card is a single playing card. Construct one with NewCard so that an
// out-of-range rank or suit can never enter the engine; a zero Card is not a
// valid playing card.
type Card struct {
	Rank Rank
	Suit Suit
}

// Sentinel errors returned by NewCard and ParseCard. Callers can match these
// with errors.Is to distinguish bad rank, bad suit, and malformed text.
var (
	ErrInvalidRank = errors.New("cribbage: rank out of range (want Ace..King)")
	ErrInvalidSuit = errors.New("cribbage: suit out of range (want Clubs..Spades)")
	ErrBadCardText = errors.New("cribbage: card text must be a rank letter followed by a suit letter")
)

// Valid reports whether r is a real rank, Ace..King.
func (r Rank) Valid() bool { return r >= Ace && r <= King }

// Valid reports whether s is a real suit, Clubs..Spades.
func (s Suit) Valid() bool { return s <= Spades }

// NewCard builds a validated Card. It is the only blessed way to make one, so
// that "is this a real card?" is answered once, at the boundary, and never
// again downstream. Out-of-range input yields ErrInvalidRank or ErrInvalidSuit.
func NewCard(rank Rank, suit Suit) (Card, error) {
	if !rank.Valid() {
		return Card{}, fmt.Errorf("%w: %d", ErrInvalidRank, rank)
	}
	if !suit.Valid() {
		return Card{}, fmt.Errorf("%w: %d", ErrInvalidSuit, suit)
	}
	return Card{Rank: rank, Suit: suit}, nil
}

// PipValue is the rank's worth when summing toward fifteen: Ace is 1, pip cards
// are their face value, and every face card (Jack, Queen, King) is 10. It is a
// total function over valid ranks.
func (r Rank) PipValue() int { return min(int(r), 10) }

// rankLetters and suitLetters index by (rank-1) and suit respectively, giving
// the single-character codes used in textual notation: T for ten, A/J/Q/K for
// the others; C/D/H/S for suits.
var (
	rankLetters = [13]byte{'A', '2', '3', '4', '5', '6', '7', '8', '9', 'T', 'J', 'Q', 'K'}
	suitLetters = [4]byte{'C', 'D', 'H', 'S'}
)

// String renders the rank as its notation letter, or "?" if the rank is
// somehow out of range (which NewCard prevents).
func (r Rank) String() string {
	if !r.Valid() {
		return "?"
	}
	return string(rankLetters[r-1])
}

// String renders the suit as its notation letter, or "?" if out of range.
func (s Suit) String() string {
	if !s.Valid() {
		return "?"
	}
	return string(suitLetters[s])
}

// String renders the card in two-character notation, e.g. "5H", "TD", "JS".
// ParseCard is its exact inverse for every valid card.
func (c Card) String() string { return c.Rank.String() + c.Suit.String() }

// MarshalJSON encodes a card as its two-character string ("5H"), so the wire
// format is the same compact notation used everywhere else.
func (c Card) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

// UnmarshalJSON decodes a card from its two-character string form.
func (c *Card) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := ParseCard(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// ParseCard is the inverse of Card.String: it reads two-character notation
// (rank letter then suit letter) into a validated Card. It exists so the
// golden scoring fixtures can store cards as compact strings. Malformed input
// yields ErrBadCardText; a good shape with an unknown letter yields
// ErrInvalidRank or ErrInvalidSuit.
func ParseCard(s string) (Card, error) {
	if len(s) != 2 {
		return Card{}, fmt.Errorf("%w: %q", ErrBadCardText, s)
	}
	rank, ok := rankFromByte(s[0])
	if !ok {
		return Card{}, fmt.Errorf("%w: %q", ErrInvalidRank, s[:1])
	}
	suit, ok := suitFromByte(s[1])
	if !ok {
		return Card{}, fmt.Errorf("%w: %q", ErrInvalidSuit, s[1:])
	}
	return Card{Rank: rank, Suit: suit}, nil
}

func rankFromByte(b byte) (Rank, bool) {
	for i, letter := range rankLetters {
		if letter == b {
			return Rank(i + 1), true
		}
	}
	return 0, false
}

func suitFromByte(b byte) (Suit, bool) {
	for i, letter := range suitLetters {
		if letter == b {
			return Suit(i), true
		}
	}
	return 0, false
}
