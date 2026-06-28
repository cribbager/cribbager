package cribbage

// Deck returns all 52 distinct cards in a fixed order (rank-major: Ace of each
// suit, then 2 of each suit, and so on). The order is deterministic so tests
// and exhaustive enumerations are reproducible.
func Deck() []Card {
	d := make([]Card, 0, 52)
	for r := Ace; r <= King; r++ {
		for s := Clubs; s <= Spades; s++ {
			d = append(d, Card{Rank: r, Suit: s})
		}
	}
	return d
}
