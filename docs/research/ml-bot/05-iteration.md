# Chapter 5 — The iteration loop, a stall, a leak, and the bar

*2026-07-08. Code: `ml/scripts/peg_iterate.sh`, per-seat policies in
`peg.Generate`, `SEED` override in the lab gate. Result: **the program's bar
is cleared** — the learned pegging policy beats champion v3 on the
pegging-isolated gate, +0.70 pts/pair (95% CI [+0.46, +0.95]) on deals it
never trained on, with win-diff non-negative (+0.007 [−0.000, +0.015]).*

## The loop, and the whole story in one table

One round = generate 20k games of self-play with the *current* net (ε=0.2
exploration) → retrain from scratch → gate. Each round's Q answers "what is
this move worth if everyone continues like round N−1".

| round | training data | margin vs champion (2000-pair gate) |
|---|---|---|
| 0 | 973k decisions, random self-play | −2.89 [−3.44, −2.34] |
| 1 | 909k, ε-greedy Q₁ self-play | −0.95 [−1.48, −0.41] |
| 2 | 934k, ε-greedy Q₂ self-play | −1.39 [−1.95, −0.84] ← **stall** |
| 3 | **pooled rounds 1+2** (1.84M), no new data | **+0.47 [−0.06, +1.01]** |
| — | round 3 net, 10,000-pair gate, virgin seeds | **+0.70 [+0.46, +0.95]** |

## Lesson 1: the stall — single-round training is a moving target

Round 2 regressed (or at best flatlined; the CIs overlap). This is the
textbook self-play instability: each round trains a FRESH net on only the
newest data, whose state distribution is whatever the previous net's habits
visit. The net drifts — improving against the states its predecessor
frequents, quietly forgetting the rest. The classic fix is memory:

## Lesson 2: pooling — cheap, boring, decisive

Round 3 trained on rounds 1+2 together — no new games at all, purely a data
diet change — and flipped the sign of the margin. Doubling the data helps,
but the bigger effect is distributional: two different behavior policies'
states force the net to be right in more of state space at once. (Round 0's
random data was deliberately excluded: returns under random continuation are
so noisy they drag value estimates toward mush. Pooling wants *comparable*
policies, not all history indiscriminately.) The trade nobody escapes:
pooled returns mix two behavior policies, so Q's implied "continuation"
is a blend — theoretically muddier, empirically far more stable. RL in five
words: variance is the real enemy.

## Lesson 3: the leak — always ask where your test deals came from

The +0.47 needed more power, and the 10,000-pair rerun said +0.75… at which
point an audit question killed the celebration: the gate's deck seeds
(1..10000, from `Compare`'s fixed seed 1) OVERLAP the training runs' deck
seeds (~11..20101). The net had trained on most of the gate's exact deals.
Train-on-test contamination — chapter 2's leakage lesson at the *evaluation*
level this time.

The fix: a `SEED` env for the gate (now permanent lab equipment, with a
comment that trained challengers must be gated on seeds disjoint from their
training), and a rerun at seed 1,000,000: **+0.70 [+0.46, +0.95]**. The
result was real — mechanistically expected, since 35k parameters looking at
rank-level features cannot memorize specific deals — but "expected" is not
"verified", and the verified number is the one that counts.

## Reading the win: what was actually beaten, and by what

The bar ("beat champion v3 at pegging") is met on its stated instrument. Two
honest qualifications:

- **Mechanism**: hypothesis 1 from the program README — one-ply search
  cannot see whole-series consequences; Monte-Carlo returns price them
  implicitly. The learner gives up ~0.04 pts/deal less than the champion's
  pegging across ~18 deals/pair.
- **Confound, bounded**: near the target the champion pegs win-aware
  (sacrificing peg points), while ml-peg is score-blind — so part of the
  points margin could be the chapter-3 harvest effect. But chapter 3 showed
  what a pure harvest looks like: points up, WINS DOWN (−0.005). Here the
  win-diff is *non-negative* (+0.007, CI grazing zero from above). The
  learner banks extra points without paying wins for them — most of the
  margin is real pegging skill, not objective arbitrage.

## Not promoted (yet)

The auto-verdict again says "promote". Holding fire deliberately: a
score-blind pegger as THE champion would regress endgame play that the
positional fixtures exist to protect, and the right promotion candidate is
probably a composed bot (champion discards + ML pegging + win-aware
endgame handling) taken through the full two-instrument process, positional
fixtures included. That is Phase 3 (assembly), now unlocked with the bar met.

Also unlocked, if wanted: targeted training. `peg.Generate` now takes
per-seat policies, so the learner can explore *against the champion itself*
(`pegdata -opponent champion`) — data answering exactly the gate's question,
plus expert demonstrations for free. Self-play optimizes for general
strength; training against the deployment opponent optimizes the actual
match-up. A chapter 6 experiment if the Phase 3 gate wants more margin.

## Reproduce

```
ml/scripts/peg_iterate.sh 1   # round 1 (expects round-0 weights installed)
ml/scripts/peg_iterate.sh 2
cat ml/data/peg-iter1.jsonl ml/data/peg-iter2.jsonl > ml/data/peg-pool12.jsonl
cd ml && uv run scripts/train_pegging.py --data data/peg-pool12.jsonl \
    --out runs/peg-r3pool --install ../internal/bot/lab/testdata/pegging-v1.json
CHALLENGE=ml-peg PAIRS=10000 SEED=1000000 go test ./internal/bot/lab -run ChallengerVsChampion -v
```
