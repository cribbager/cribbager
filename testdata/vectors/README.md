# Shared rules test vectors

Language-neutral test vectors exported from the Go engine, which is the
reference implementation. Other implementations (the Swift `CribbageEngine` in
the iOS app) load these files and must reproduce every expectation; a mismatch
means the rules have drifted.

**Do not edit by hand.** They are generated deterministically (fixed seeds) by
`internal/vectors`:

```bash
go run ./cmd/vectors        # rewrite testdata/vectors/*.json
go test ./internal/vectors  # fails if the committed files are stale
```

Copy the three `.json` files (and this README) into the consuming repo. Each
file is `{"version", "kind", "seed", "count", "vectors": [...]}`, one vector per
line. `version` changes only on an incompatible schema change.

## Card encoding

`<rank><suit>`: rank is `A 2 3 4 5 6 7 8 9 10 J Q K`, suit is `C D H S`.
Examples: `AS`, `10H`, `JC`. The ten is `10` (the Go engine's internal notation
writes `T`; the vectors do not). Ace is always low; there is no wraparound run.

## `hand_scoring.json`

Counting a four-card hand plus starter (ACC rules).

| field | meaning |
|---|---|
| `name` | optional description (canonical cases only) |
| `hand` | the four hand cards |
| `starter` | the cut card |
| `isCrib` | crib flushes need all five cards the same suit; hand flushes need four |
| `total` | the score |
| `fifteens` | points from fifteens (2 each) |
| `pairs` | points from pairs, including pairs inside a double/triple run |
| `runs` | points from runs only: `runLength * runCount` |
| `runLength`, `runCount` | length of the run and the number of distinct runs (2 = double run, 3 = triple, 4 = double-double); 0 if no run |
| `flush` | 0, 4 or 5 |
| `nobs` | 0 or 1 |

`total = fifteens + pairs + runs + flush + nobs`. Contents: hand-picked
canonical cases (29, 28, 0, double/triple/double-double runs, 4- vs 5-card
flush in hand and crib, nobs, no wraparound), plus seeded random hands:
uniform, flush-rich, run-rich and jack-rich.

## `pegging.json`

A single count sequence: cards played in order, alternating players, with no
reset inside it (always at most 31).

| field | meaning |
|---|---|
| `plays` | cards in order of play |
| `steps` | one `[count, total, fifteen, thirtyOne, pair, run]` per play |

`count` is the running count after the card; `fifteen`/`thirtyOne` are 0 or 2;
`pair` is 0, 2, 6 or 12 (pair, pair royal, double pair royal); `run` is 0 or
the run length (3+), which is also its points. A run is the longest tail of the
count that is consecutive in any order. `total` is the sum of the other four.
Go and last-card points belong to a hand's flow, not to one play, so they are
covered by `game_flow.json`.

## `game_flow.json`

One deal played from a scripted position, replayed through the game state
machine. Seats are `0` and `1` (the Swift engine's `Player.one` and `.two`).

Inputs: `dealer`, `scores` going in, `hands` (each seat's six dealt cards),
`discards` (each seat's two crib cards), `starter` (the cut), and `plays`
(each pegging play in order, with the seat that makes it; the engine decides
whose turn it is, so the seats are themselves a check on go/turn handling).

Expectations: `events`, `finalScores`, `winner` (0, 1, or -1 if the hand
finished with no winner). Events, in order:

| type | fields | meaning |
|---|---|---|
| `heels` | seat (the dealer), points 2 | jack starter |
| `play` | seat, card, points | points the play scored (15/31/pair/run) |
| `go` | seat, points 1 | go or last card; never awarded on a 31 |
| `hand` | seat, points | a hand counted at the show (pone first, then dealer) |
| `crib` | seat (the dealer), points | the crib counted |
| `won` | seat | reached 121; no event follows it, so later hands/crib are never counted |

`points` is omitted when 0. Scores stop at 121 (an overshooting count still
ends on exactly 121). The engines' passes and count resets are not events in
this contract because the two models represent them differently. Covers:
winning exactly on 121 and overshooting it in a play, on a go point, on the
pone's hand, the dealer's hand and the crib; his heels (including winning on
it); and seeded random deals from varied scores.
