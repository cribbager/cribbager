# Chapter 7 — Phase 4: the discard learns from outcomes

*2026-07-09. Code: `cmd/mldata -mode outcomes`, `ml/scripts/train_discard_mc.py`,
lab bot `mc-discard`, `BASELINE` option on the lab gate.*

Every discard decision so far — champion's and chapter 2's net alike — has
been scored by an evaluator that cannot see pegging: `ehand + crib` is exact
about the shows and silent about the play. Chapter 5 made that blind spot
expensive to ignore: pegging is now played by a net that extracts +0.70
pts/pair more than the old assumptions priced in. Hypothesis 2 says keeps
have pegging value the champion's evaluator provably cannot rank (its
pegging term is hold-independent). This chapter tests it.

## Lesson: exact-but-partial versus complete-but-noisy

Two ways to label a discard decision:

- **Chapter 2**: the evaluator's expectation. Exact (zero noise), infinite
  supply — and *partial*: pegging simply isn't in the number. A net trained
  on it can at best tie the evaluator (it did: 96% agreement).
- **Chapter 7**: what the deal then actually delivered — own deal points
  minus the opponent's, pegging included, under the production bot's
  pegging. *Complete*: every real consequence of the keep is in the label.
  And noisy: the starter cut, the opponent's unseen hand, and every pegging
  exchange contribute variance that has nothing to do with the decision.

The trade is volume. Returns have a standard deviation of roughly 9 points
while the differences between a hand's splits are fractions of a point — but
the argmax only needs the *ranking* of 15 splits to come out right, and a
million labeled decisions average a lot of noise.

Mechanics, all inherited from earlier chapters: ε-exploration around the
champion's discards (ε=0.25 uniform splits — the trainer must see where bad
keeps lead, chapter 4's lesson), returns from complete deals only (deals
truncated by a game ending are censored — their outcome never fully
happened), the same 105-dim encoding whose Go/Python duality is already
parity-fixed (chapter 3), train/val split by game (chapter 2).

## Lesson: isolate the decision you're testing

The gate gained a `BASELINE` option: `CHALLENGE=mc-discard BASELINE=ml`
compares outcome-net discards + ml pegging against champion discards + ml
pegging. Both sides peg identically, duplicate deals cancel the cards, so
the paired margin measures the *discard policy alone*, in the world where
pegging is played the way production actually plays it. (Compared against
the full champion instead, any discard signal would be tangled with the
pegging difference we already know about.)

## Round 0: whole-deal outcomes — a proper failure

60k games, 1,054,440 decisions (9% censored). The net trained fine (val MSE
≈ the irreducible noise floor) and the bot was a disaster: **−11.83 pts/pair
[−12.20, −11.47]** on the discard-isolated gate. Diagnosis before redesign:
scored against chapter 2's exact labels, the outcome net agreed with the
exact argmax only 50%/46% of the time, giving up 0.75 pts/hand — it learned
the coarse value structure from pure outcomes (random would be 7%) but the
fine structure never surfaced through ±9 points of label noise. And the
arithmetic closes: 0.75 pts/hand × ~16 hands/pair ≈ the −11.8 observed.
Not a bug — a variance failure, the one the program README predicted.

## Round 1: residual learning — don't relearn what you know exactly

The fix is the classic control-variates move. Decompose a deal's outcome:
hand and crib have EXACT expectations (chapter 2's tables); the only
component the evaluator cannot rank per hold is pegging. So the net's target
became the deal's realized **pegging differential only** (noise sd ~3.5, not
~9), and the policy became `exact Score + predicted pegging` — worst case,
it degrades to the champion's point-EV discard instead of below it.

| discard policy (ml pegging both sides, vs BASELINE=ml, 10k pairs) | margin | windiff |
|---|---|---|
| round 0: whole-deal outcome net | −11.83 [−12.20, −11.47] | −0.219 |
| **round 1: exact + pegging-residual net** | **+0.78 [+0.55, +1.02]** | −0.000 [−0.008, +0.008] |
| control: exact point-EV, no net | +0.38 [+0.24, +0.52] | −0.010 [−0.015, −0.005] |

The control (ev-discard) isolates the chapter-3 "harvest" effect — a
score-blind discarder banking points the champion's win objective spends —
so the decomposition reads: **learned pegging-awareness ≈ +0.40 pts/pair of
genuine discard skill** (0.78 − 0.38), which also wins back the harvest's
−0.010 win cost. Hypothesis 2 confirmed.

## The endgame question — and an inverted answer

Is score-blindness safe for discards? Two candidates measured:

- An `InReach` handoff to the champion's win-prob discard (the chapter-6
  recipe) kept the endgame "safe" and destroyed the margin: +0.13 [−0.03,
  +0.30] vs BASELINE=ml — below the "bolt-on tweaks don't pay" bar. The
  handoff region covers ~half the game, and much of the net's value lives
  there.
- The score-blind variant, put to the **positional fixtures** (BASELINE=ml,
  endgame power, virgin seeds): pooled windiff **+0.008 [+0.002, +0.013] —
  it WINS MORE from the fixture positions**, peaking at +0.043 [+0.023,
  +0.063] from 118-118, where the game is nothing but pegging. The
  theory-fear didn't just fail to materialize; it had the sign backwards.
  Pegging-aware keeps beat the win-walk's hold-independent pegging
  approximation exactly where pegging decides games. (Contrast chapter 6:
  for PEGGING the handoff was worthless-but-harmless; for DISCARDS it is
  actively expensive. Same boundary, opposite verdicts — instruments over
  theory, every time.)

## Promotion: production ml v2

The score-blind exact+residual discard shipped as **ml v2** (chapter 6's bot
with its discard upgraded; both networks embedded; `bot.DiscardInput` is now
the shared canonical encoder, still pinned by the lab parity fixture). Gates
passed: +0.78 pts/pair at win-parity (discard-isolated, virgin seeds) and
+0.008 wins/pair pooled fixtures. For the whole package vs the champion:
**+0.75 pts/pair [+0.49, +1.02] and windiff +0.011 [+0.003, +0.019]** — the
first bot to beat the champion on BOTH instruments, wins included.

Lab bookkeeping per convention: mc-discard (handoff, lost), mc-discard-blind
(promoted), and ev-discard (control) all deleted; this chapter and git
history are the record. The gates gained `BASELINE` (both the full-game gate
and the fixtures), which is what made the discard-isolated instrument and
the decomposition possible.

## Where the program stands

- Hypothesis 1 (series pegging tactics): confirmed, shipped (ml v1).
- Hypothesis 2 (pegging-aware keeps): **confirmed, shipped (ml v2)** — worth
  ≈ +0.40 pts/pair and the fixture wins.
- Hypothesis 3 (early-game positional discarding): still open — needs a
  win-based target for the far-from-end phase; the outcome pipeline built
  here is most of the machinery.

## Reproduce

```
go run ./cmd/mldata -mode outcomes -n 60000 -seed 5000000 -out ml/data/discard-mc-60k.jsonl
cd ml && uv run scripts/train_discard_mc.py --data data/discard-mc-60k.jsonl \
    --target peg_diff --out runs/discard-mc-v2 --install ../internal/bot/lab/testdata/discard-mc-v1.json
CHALLENGE=mc-discard-blind BASELINE=ml PAIRS=10000 SEED=2000000 \
    go test ./internal/bot/lab -run ChallengerVsChampion -v   # (bots deleted post-promotion; git history)
CHALLENGE=mc-discard-blind BASELINE=ml PAIRS=1500 SEED=3500000 \
    go test ./internal/bot/lab -run ChallengerFixtures -v
```
