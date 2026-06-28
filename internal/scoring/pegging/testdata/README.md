# Pegging golden vectors

Golden test vectors that pin the pegging scorer's behavior as *data*. The
package test (`score_test.go`) scores every case and checks it against
`total`/`points`.

## `pegging_scores.json`

An array of single-play cases for the play (pegging) phase. Each entry:

| field | meaning |
|---|---|
| `name` | human description of what the case exercises |
| `series` | cards already played in the current count sequence, in order |
| `card` | the card just played |
| `count` | the running count after the card is played (≤ 31) |
| `total` | the points the play scores |
| `points` | per-event points: `fifteen`, `thirtyOne`, `pair`, `run` |
| `runLength` | present only for plays that make a run |

**Card codes:** rank ∈ `A 2 3 4 5 6 7 8 9 T J Q K`, suit ∈ `C D H S`
(e.g. `5H`, `TD`, `JS`). Ace is always low.

`total` equals `fifteen + thirtyOne + pair + run`. Allowed values:
`fifteen`/`thirtyOne` ∈ {0, 2}, `pair` ∈ {0, 2, 6, 12}, `run` is 0 or ≥ 3.
These cases score only a single card's play — the 1-point "go" and "last card"
are awarded by the game engine, not the scorer.
