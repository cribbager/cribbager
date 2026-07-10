# Chapter 4 — Pegging: no more teacher

*2026-07-08. Code: `internal/bot/lab/peg/`, `cmd/pegdata`,
`internal/bot/lab/pegbot.go`, `ml/scripts/train_pegging.py`.*

Phase 1 had a luxury Phase 2 lacks: someone to copy. There is no exact
evaluator for pegging — the champion's one-ply search is itself an
approximation, and imitating it would inherit its ceiling. From here the net
learns from *consequences*: reinforcement learning.

## Lesson 1: pegging as a (partially observable) Markov decision process

The RL frame has three parts, and pinning them down IS the design work:

- **State** — what the deciding seat legitimately sees mid-play: its remaining
  cards, the series so far, the count, everything already played. The
  opponent's exact holding is hidden — that's the "partially observable" part —
  but the observable evidence (which ranks are already accounted for, how many
  cards they have left) summarizes it. We encode exactly that and nothing
  more; a bot that saw hidden state would be learning to cheat.
- **Action** — a *rank*, not a card. Suits never score during the play, so all
  suits of a rank are the same move: the action space is 13, not 52. Halving
  (quartering) the action space this way is free sample efficiency — the net
  never has to discover that the 7♣ and 7♥ do the same thing.
- **Reward** — pegging points, signed: + what I score, − what they score. A
  decision's **return** (G) is the sum of rewards from that decision to the
  end of the deal's play. Undiscounted: episodes are a handful of plies, and
  we genuinely care about the last card's point exactly as much as the first's.
  Deliberately score-blind (pure differential) in v1, same reasoning as the
  discard net: win-awareness is a later, separate lesson.

## Lesson 2: Monte-Carlo Q-learning, the simplest thing that can work

The net maps state → 13 outputs: Q(s, a) ≈ the expected return of playing
rank a from state s. Training data is self-play; each logged decision
becomes one regression example: push Q(s, a_taken) toward the return G that
*actually followed*. Two properties matter:

- **Only the taken action learns from a row.** The other 12 outputs get no
  gradient. Coverage of the action space therefore comes entirely from the
  behavior policy's exploration — this is why round 0 plays uniformly at
  random (maximum exploration, zero prior), and later rounds keep an ε of
  randomness rather than playing pure greedy.
- **Q is relative to the behavior policy.** A Q fitted on random self-play
  answers "what happens after this move *if everyone plays randomly from here
  on*". Acting greedily against that Q is provably an improvement over random
  (the policy improvement theorem) but it is NOT optimal play — it
  over-values traps a competent opponent would never fall into. Fixing that
  is the iteration loop: generate data with the improved policy, refit,
  repeat. Each round's Q answers "…if everyone plays like round N−1".

Monte-Carlo returns (full-episode sums) rather than TD bootstrapping is a
deliberate simplicity choice: no target networks, no moving-target
instability, at the cost of higher variance per example — which we pay down
with volume, because the engine deals millions of decisions per minute.

Forced moves (one distinct legal rank) are not logged: with no choice there
is nothing to learn from the choice.

## Lesson 3: the encoder lives where inference lives

Chapters 1–3 needed parity fixtures because the *encoding* existed twice
(Python training, Go play). This time the encoder is written once, in Go
(`peg.Encode`), the generator emits already-encoded vectors, and Python
trains on them opaquely. The whole class of cross-language encoding bugs is
gone by construction — the forward-pass parity from chapter 1 is the only
bridge left, and it's already under test. This is chapter 1's lesson
*applied as architecture*, not just remembered.

The 128 dims: my hand as rank-counts (13), the last five series cards as
ordered rank one-hots (65 — order matters, runs and pairs live here), the
count one-hot (32 — 15 and 31 are cliffs, a scalar would blur them), the
opponent's remaining-card count (5), ranks unavailable to the opponent (13).

## The instrument, and the gap to close

`pegBot` (lab) pairs the champion's discards with an experimental pegging
policy, so a duplicate-deal compare against the champion isolates pegging
skill: identical discards → identical shows (modulo game-end truncation) →
the paired margin is pegging differential. Two registered challengers:

- `peg-random` — champion discards + uniform random pegging: the floor. Its
  gate margin measures the total room between the worst pegging and the
  champion's.
- `ml-peg` — champion discards + greedy-Q pegging: the learner.

## Results, round 0

Data: 20,000 self-play games, both seats pegging uniformly at random —
187,150 deals, 973,449 non-forced decisions. (Side-finding: even under
random play the dealer pegs 2.77 pts/deal to the pone's 1.71 — the seat
advantage is structural, not skill.) Q₁: 128→128→128→13, ~35k params, 12
epochs, ~15s. Val MSE 4.71 against return variance 6.5 — the net explains
~28% of return variance, which is *healthy*, not disappointing: under random
continuation most of a return is the opponent's future dice rolls,
irreducible noise. What matters is the *ranking* of the 13 Q-values, and the
gate measures exactly that:

| pegging policy (champion discards) | margin vs champion (95% CI) | win rate |
|---|---|---|
| random (`peg-random`) | −21.71 pts/pair [−22.39, −21.02] | 29.9% |
| greedy Q₁ (`ml-peg` v1) | **−2.89 pts/pair [−3.44, −2.34]** | 46.6% |

One round of fitted-Q on random self-play closed **87% of the pegging gap**.
This is the classic RL shape: the first policy-improvement step captures
most of the value (don't break runs for the opponent, pair when it pays,
take your 15s and 31s), because most of random's −21.7 was point-blank
blunders that even a "what happens next if everyone flails" value function
sees clearly.

The remaining −2.9 pts/pair is the hard part, and its cause is Lesson 2's
caveat made flesh: Q₁ believes the opponent plays randomly. It walks into
traps a one-ply searcher with a calibrated belief never sets off, and it
declines profitable risks a random opponent would punish but a real one
cannot. Closing that gap is the iteration loop — regenerate data with
ε-greedy Q₁ self-play, refit, repeat until the gate stops moving — which is
chapter 5, along with its own new failure modes (policy collapse, forgetting,
the ε schedule).

## Reproduce

```
CHALLENGE=peg-random PAIRS=2000 go test ./internal/bot/lab -run ChallengerVsChampion -v
go run ./cmd/pegdata -games 20000 -seed 11 -policy random -out ml/data/peg-iter0.jsonl
cd ml && uv run scripts/train_pegging.py --data data/peg-iter0.jsonl \
    --out runs/peg-v1 --install ../internal/bot/lab/testdata/pegging-v1.json
CHALLENGE=ml-peg PAIRS=2000 go test ./internal/bot/lab -run ChallengerVsChampion -v
```
