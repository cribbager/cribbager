# How the bot decides

What the champion bot actually computes when it discards to the crib and when it
plays a card during pegging. For running games against it, see
[Development](development.md); the code lives in `internal/bot` (the bot) and
`internal/bot/eval` (the evaluators it composes).

Three properties frame everything below:

- **It cannot cheat.** The bot decides from `game.PlayerView` — the same
  visibility-filtered state a human at that seat sees. Hidden cards physically
  aren't in its input.
- **It is deterministic.** Same view in, same move out. There is no sampling at
  decision time; everything probabilistic was precomputed into tables.
- **It maximizes wins, not points.** Away from the end of the game those are the
  same thing, and the bot plays exact point expected value. Once either player is
  within reach of 121, it switches objective to the probability of winning —
  which is where "take risks when behind, play safe when ahead" comes from,
  with no hand-written rules.

The champion is versioned (`bot.Version`, recorded with every finished game):
v1 was point-EV with a uniform opponent model, v2 added the calibrated opponent
model, v3 added the win-probability objective.

## Discarding: which two cards go to the crib

`eval.RankDiscardsWin` scores all 15 ways to split the six dealt cards into
keep-four / throw-two.

### The point-EV core (always computed)

Each hold gets `Score = EHand + Crib`:

- **`EHand`** — the exact average show score of the kept four, over every one of
  the 46 cards the starter could be (`eval.ExpectedHandValue`). Not an estimate:
  all 46 futures are scored and averaged.
- **`Crib`** — the exact expected value of the two thrown cards in the crib,
  assuming the opponent's two crib cards and the starter are uniform over the
  remaining 50 (`eval.CribEV`). This was enumerated offline for every rank pair
  (suited and unsuited) into a lookup table, so at decision time it's O(1).
  The term is **signed**: added when it's the bot's crib, subtracted when it's
  the opponent's. That sign flip alone reproduces most classic discard lore —
  "never throw a 5 to their crib" simply falls out of the numbers.

Far from the end of the game, the best hold by this score is the answer, and
the classic result holds: the maximum-points *hand* is often not the best
*hold* once the crib term is priced in.

### The win-probability layer (in reach of 121)

When either player could plausibly reach 121 this deal (`farFromEnd` is false —
the threshold is each role's maximum possible one-deal gain, derived from the
tables), points stop being the objective: 30 points past 121 win exactly as
hard as 1, and the **order** points land in decides games. Cribbage counts in a
fixed order — pegging as it happens, then the pone's hand, then the dealer's,
then the crib — so the same totals can produce opposite winners.

For each hold, `eval.RankDiscardsWin` walks the coming deal in counting order
(`windiscard.go`), carrying a joint probability grid over (my points gained,
opponent points gained):

1. **His heels** — 2 to the dealer, at the measured probability.
2. **Pegging** — the joint (dealer pegging, pone pegging) distribution measured
   from champion self-play; within it the pone's points count first (the pone
   leads).
3. **Pone's hand**, then **dealer's hand** — the bot's own hand uses the *exact*
   score distribution of the kept four over all 46 starters
   (`eval.HandValueDist`); the opponent's hand uses the self-play marginal.
4. **The crib** — the exact score distribution for the thrown pair under a
   uniform completion (a distribution-valued sibling of the `CribEV` table).

The moment a cumulative gain crosses a player's need, that branch resolves as a
win or a loss — whoever crosses **first** wins, which is exactly the counting-
order rule. Probability mass that survives the whole deal continues into the
next deal at the win-probability table's value for the new scores (with the
deal rotated). Holds are ranked by the resulting P(win), with point EV breaking
near-ties.

This is what makes endgame discards look different from mid-game ones: behind
90–117, a hold that scores 8-or-nothing beats a flat safe 4 even at equal
average, because only the fat tail wins the game; ahead late, the same math
prefers the hold that denies the opponent's crib its best completions.

## Pegging: which card to play

`eval.RankPlaysWin` scores every legal card. Two ingredients: what the play
scores now, and what the opponent's reply is likely to score — priced by a
**calibrated belief** about which cards the opponent actually holds.

### The calibrated opponent model

The naive model says: the opponent's unseen cards are a uniform draw from
everything I can't see. The bot does better, three ways (`eval/belief.go`):

- **It remembers its own crib throw.** Those two cards are in the unseen pool
  by visibility rules, but they cannot be in the opponent's hand — they're
  excluded outright (`PlayerView.YourDiscards`).
- **It knows how people discard, because it knows how *it* discards.** Each
  unseen card is weighted by the probability that the champion's own discard
  policy would have *kept* that rank, measured offline over a million dealt
  hands (`eval/keepprob.go`). The table is role-aware and matches intuition:
  throwing to the **opponent's** crib, 5s are kept 97.2% of the time and kings
  only 40.1%; throwing to your **own** crib the profile flattens (5s 71.9%).
  So an unseen 5 is far more likely to be in the opponent's hand than a uniform
  draw says — and the bot prices its plays accordingly.
- **A "go" is hard evidence.** If the opponent passed in the live series at
  count *c*, they hold nothing with pip value ≤ 31−*c*. The bot reconstructs
  this from the view alone (pile ownership) and zeroes those cards exactly.

The weights are scaled into per-card inclusion probabilities summing to the
opponent's known hand size, and the expected value of their **best** reply is
computed in closed form over the whole pool (every way each candidate pile can
be punished, weighted by how likely the opponent holds a card that does it).

### Choosing the card

Away from the end, each legal card is scored one ply deep:

```
Score = points it pegs now − E[opponent's best reply] (+ a tiny keep-low-cards tie-break)
```

A play that makes 31 has no reply and is priced accordingly. This simple depth-1
rule against the calibrated belief is stronger than it looks: it independently
agrees with all eight classic expert pegging heuristics (don't lead a 5, don't
make the count 5 or 21, lead low, keep a low card for the go, …) — see the
audit in `eval/heuristics_audit_test.go`.

In reach of 121 the objective flips to P(win): the bot's own points land first
(pegging out is a certain win, taken before any reply exists), then each
possible opponent reply either counts *them* out or moves the game to a new
score whose win probability comes from the table. The card with the highest
resulting P(win) is played, with net point EV breaking ties.

## The win-probability table

Both endgame objectives share one table (`eval/winprob.go`):
`P(the player about to deal wins | dealer score, pone score)`, all 121×121
states. It is built offline (`winprob_gen.go`) by:

1. playing 20,000 champion self-play games and keeping every complete deal as
   its sequence of scoring increments **in counting order** (~166k deals, ~87k
   distinct sequences) — the engine's event log order *is* the counting order;
2. exact dynamic programming over those observed sequences: from any score
   pair, walk each weighted sequence; whoever crosses 121 first wins, and
   undecided deals recurse into the table with the deal rotated. Every deal
   scores at least one point, so a single pass in decreasing score-total order
   suffices — no iteration.

The table checks out against reality: it independently reproduces the measured
first-dealer edge (0.5625 vs 0.561 measured), and it encodes real endgame
structure — at **115–115 the pone is favored** (the show decides, and the pone
counts first), while at **120–120 the dealer is favored** (pegging decides, and
the dealer scores first, replying to the lead).

## Honest approximations

Each accepted deliberately, tested where possible:

- The opponent-reply model treats card inclusions as independent rather than a
  draw without replacement; a brute-force oracle bounds the error and shows it
  slightly *understates* replies uniformly, which barely disturbs ranking.
- The discard deal-walk treats components (pegging, hands, crib) as independent
  and counts all pone pegging before all dealer pegging within a deal.
- Mid-deal win probabilities reuse the deal-start table.
- The opponent's hand and pegging distributions are self-play marginals, not
  conditioned on the current hand.

## How the champion changes

There is only ever one shipped bot. Improvements are developed as challengers
in `internal/bot/lab` and must beat the champion over thousands of duplicate
deals (same decks, seats swapped, so card luck cancels) before being folded in:
point-EV changes gate on the paired **points margin** CI clearing zero;
score-aware changes gate on the paired **win-difference** CI plus positional
fixtures started at endgame scores — because a bot that correctly trades points
for wins looks *worse* on points. Losers are deleted; git history is the
archive. See `internal/bot/compare.go` and `internal/bot/lab/`.
