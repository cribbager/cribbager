package cribbage

import (
	"errors"
	"testing"
)

func TestNewCardValid(t *testing.T) {
	c, err := NewCard(Ace, Spades)
	if err != nil {
		t.Fatalf("NewCard(Ace, Spades) returned error: %v", err)
	}
	if c.Rank != Ace || c.Suit != Spades {
		t.Fatalf("got %+v, want {Ace Spades}", c)
	}
}

func TestNewCardRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		rank    Rank
		suit    Suit
		wantErr error
	}{
		{"rank zero", 0, Clubs, ErrInvalidRank},
		{"rank above King", 14, Clubs, ErrInvalidRank},
		{"suit above Spades", Ace, 4, ErrInvalidSuit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCard(tc.rank, tc.suit)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("NewCard(%d, %d) error = %v, want %v", tc.rank, tc.suit, err, tc.wantErr)
			}
		})
	}
}

func TestPipValue(t *testing.T) {
	// Ace..9 are face value; every ten-or-higher rank is worth 10 toward fifteen.
	want := map[Rank]int{
		Ace: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 6, 7: 7, 8: 8, 9: 9,
		10: 10, Jack: 10, Queen: 10, King: 10,
	}
	for r := Ace; r <= King; r++ {
		if got := r.PipValue(); got != want[r] {
			t.Errorf("Rank(%d).PipValue() = %d, want %d", r, got, want[r])
		}
	}
}

func TestRankString(t *testing.T) {
	want := []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "T", "J", "Q", "K"}
	for i, w := range want {
		r := Rank(i + 1)
		if got := r.String(); got != w {
			t.Errorf("Rank(%d).String() = %q, want %q", r, got, w)
		}
	}
	if got := Rank(0).String(); got != "?" {
		t.Errorf("Rank(0).String() = %q, want %q", got, "?")
	}
}

func TestSuitString(t *testing.T) {
	want := map[Suit]string{Clubs: "C", Diamonds: "D", Hearts: "H", Spades: "S"}
	for s, w := range want {
		if got := s.String(); got != w {
			t.Errorf("Suit(%d).String() = %q, want %q", s, got, w)
		}
	}
	if got := Suit(4).String(); got != "?" {
		t.Errorf("Suit(4).String() = %q, want %q", got, "?")
	}
}

func TestCardStringSamples(t *testing.T) {
	tests := []struct {
		card Card
		want string
	}{
		{Card{5, Hearts}, "5H"},
		{Card{10, Diamonds}, "TD"},
		{Card{Jack, Spades}, "JS"},
		{Card{Ace, Clubs}, "AC"},
		{Card{King, Hearts}, "KH"},
	}
	for _, tc := range tests {
		if got := tc.card.String(); got != tc.want {
			t.Errorf("%+v.String() = %q, want %q", tc.card, got, tc.want)
		}
	}
}

// TestParseCardRoundTrip is the key property: ParseCard is the exact inverse of
// String for all 52 cards. This guards the testdata notation in both
// directions at once.
func TestParseCardRoundTrip(t *testing.T) {
	count := 0
	for r := Ace; r <= King; r++ {
		for s := Clubs; s <= Spades; s++ {
			c := Card{Rank: r, Suit: s}
			got, err := ParseCard(c.String())
			if err != nil {
				t.Fatalf("ParseCard(%q) error: %v", c.String(), err)
			}
			if got != c {
				t.Errorf("ParseCard(%q) = %+v, want %+v", c.String(), got, c)
			}
			count++
		}
	}
	if count != 52 {
		t.Fatalf("covered %d cards, want 52", count)
	}
}

func TestDeck(t *testing.T) {
	d := Deck()
	if len(d) != 52 {
		t.Fatalf("Deck() returned %d cards, want 52", len(d))
	}
	seen := map[Card]bool{}
	for _, c := range d {
		if !c.Rank.Valid() || !c.Suit.Valid() {
			t.Errorf("Deck() contains invalid card %+v", c)
		}
		if seen[c] {
			t.Errorf("Deck() contains duplicate %s", c)
		}
		seen[c] = true
	}
	if len(seen) != 52 {
		t.Fatalf("Deck() has %d distinct cards, want 52", len(seen))
	}
}

func TestParseCardRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr error
	}{
		{"empty", "", ErrBadCardText},
		{"one char", "5", ErrBadCardText},
		{"three chars", "10H", ErrBadCardText},
		{"bad rank letter", "XH", ErrInvalidRank},
		{"bad suit letter", "5X", ErrInvalidSuit},
		{"lowercase suit", "5h", ErrInvalidSuit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCard(tc.text)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParseCard(%q) error = %v, want %v", tc.text, err, tc.wantErr)
			}
		})
	}
}
