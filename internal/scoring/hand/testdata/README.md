# Hand-scoring golden vectors

Golden test vectors that pin the hand scorer's behavior as *data*. The package
test (`score_test.go`) scores every case and checks it against `total`/`points`.

## `hand_scores.json`

An array of hand-scoring cases. Each entry:

| field | meaning |
|---|---|
| `name` | human description of what the case exercises |
| `hand` | the four cards in hand, as two-char codes (see below) |
| `starter` | the cut card |
| `isCrib` | whether scoring is for the crib (changes flush rules) |
| `total` | the correct total score |
| `points` | per-category subtotals (see bundling note) |
| `runLength`, `multiplicity` | present only for cases with a run |

**Card codes:** rank ∈ `A 2 3 4 5 6 7 8 9 T J Q K`, suit ∈ `C D H S`
(e.g. `5H`, `TD`, `JS`). Ace is always low.

**Bundling note on `points`.** A run with duplicate ranks is reported as one
combo whose points already include the pair(s) the duplication creates. So in
`points`, `runs` includes those bundled pairs and `pairs` counts only pairs that
are *not* part of a run. For example a double run of three contributes
`runs: 8` (six for the two runs, two for the bundled pair) and `pairs: 0`.
`total` always equals `fifteens + pairs + runs + flush + nobs`.
