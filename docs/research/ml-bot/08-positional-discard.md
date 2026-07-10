# Chapter 8 — Hypothesis 3: does early-game position bend the discard?

*2026-07-10. Code: `cmd/mldata -mode win`, `bot.DiscardInputWin`,
`ml/scripts/train_discard_win.py`, lab bot `win-discard`.*

The last open hypothesis. Far from the target, every discard in this program
— champion's and ml v2's alike — maximizes expected points, justified by the
"effectively affine" assumption: over one deal's range, more points ≈ more
win probability, so the orderings agree. Par-hole theory (Colvert's Theory
of 26 — the same theory this repo trusts as its strength benchmark) says the
win surface has curvature that experts play from the first deal: positions
just short of par differ in value from positions just past it, and the right
keep depends on where you stand, not just how many points it's worth.

If that curvature is real *and learnable*, a net with a win-based target and
score inputs will find it. If the affine assumption is right, the net will
reproduce the point ordering and the gate will read zero. Either answer
closes the question — that's what makes it a good experiment.

## Lesson: making a win target trainable at all

A naive win label (did the game end in a win?) is hopeless: binary noise,
sd ~0.5, against split differences worth ~0.001–0.01 win probability. Three
variance moves, all built on earlier chapters:

1. **Bootstrap through the value table.** The label is the deal's
   *win-probability delta*: `WinProb(end-of-deal scores, next dealer) −
   WinProb(start-of-deal scores, dealer)`. One deal of randomness instead of
   a whole game's. The price: the WinProb table (champion self-play, deal
   boundaries) is now baked into the target — the net learns curvature *as
   that table sees it*. A bonus over chapter 7: when a deal ends the game,
   WinProb clamps to exactly 1 or 0, so **nothing is censored**.
2. **The exact split score is an input feature** (chapter 7's lesson as
   feature engineering): the net starts from "points are worth what points
   are worth" and only has to learn how position bends that.
3. **Scores enter as scalars AND coarse buckets**, so carving the surface
   into par regions doesn't have to be done by bending scalars alone.

Encoding is Go-only, pre-encoded in the data (chapter 4's pattern):
`bot.DiscardInputWin`, 128 dims. Behavior policy is the production ml bot
itself with ε=0.25 random splits — on-policy for the bot this would improve.

## The candidate and the instrument

`win-discard` (lab): far from the end, argmax of predicted win delta over
all 15 splits; in reach, the production discard unchanged — so the gate
isolates precisely the early-game question. Baseline `ml`, both sides peg
identically. **The wins instrument is primary**: a positional edge is
allowed to spend points (it is the anti-harvest), so the margin may
legitimately read negative.

Prediction, on the record before the gate: the honest expectation is a
small or null effect — the affine approximation was accepted because the
early-game curvature is mild, and the win-delta noise (sd ≈ 0.1 per label
against signals of ~0.002) is the hardest ratio the program has attempted.
A confident zero is a fine way to close the program's last hypothesis.

## Results: hypothesis 3 rejected — a clean negative

80,000 games, 1.53M decisions, nothing censored. Training converged to a
val MSE of 0.0233 (residual sd ≈ 0.15 win-prob units) — the first tell:
most of a deal's win delta is the cards and the pegging, not the discard
choice, so the net explains little variance and everything it "learns"
about split differences sits under that noise floor.

20,000-pair gate (BASELINE=ml, virgin seeds):

| | margin | windiff |
|---|---|---|
| win-discard vs ml | **−0.58 [−0.70, −0.45]** | **−0.011 [−0.015, −0.007]** |

Not a null — significantly worse on BOTH instruments, including the wins
instrument the candidate was optimizing for. The reading: wherever the net's
argmax deviated from the exact+peg ordering, the deviation was
noise-hallucinated structure, and each one spent real points (−0.58) while
buying no position (−0.011 wins). If early-game par-hole curvature exists,
its per-decision value is smaller than what a one-deal-bootstrapped win
target can resolve at this scale — the signal-to-noise ratio (~0.002 signal
against 0.15 residual sd) was the hardest the program attempted, and it was
too hard.

**Scope of the negative**: this rejects "learnable at 80k games with a
one-deal bootstrap through the champion's WinProb table", not "no curvature
exists". A future attempt would need either far more data, a
lower-variance target (e.g. a distributional deal model conditioned on the
keep), or exact positional analysis instead of learning. For this program:
the affine assumption survives its first learned challenge, and ml v2's
discard stands.

Per lab convention the losing bot is deleted; the equipment stays —
`cmd/mldata -mode win` (uncensored win-delta labeling), `bot.DiscardInputWin`
(the position-aware encoding), and the trainer are reusable for any future
positional experiment.

## The program's ledger closes

1. Series-level pegging tactics — **confirmed, shipped** (ml v1, +0.70 pts/pair).
2. Pegging-aware keeps — **confirmed, shipped** (ml v2, ≈ +0.40 pts/pair + fixture wins).
3. Early-game positional discarding — **rejected at this method and scale**,
   documented above.

Two confirmed and shipped, one honestly falsified: the experiment the
program was commissioned to run ("I don't know if an ML bot would be better
— this is why I want to do this experiment") is complete.

## Reproduce

```
go run ./cmd/mldata -mode win -n 80000 -seed 6000000 -out ml/data/discard-win-80k.jsonl
cd ml && uv run scripts/train_discard_win.py --data data/discard-win-80k.jsonl \
    --out runs/discard-win-v1 --install ../internal/bot/lab/testdata/discard-win-v1.json
CHALLENGE=win-discard BASELINE=ml PAIRS=20000 SEED=4000000 \
    go test ./internal/bot/lab -run ChallengerVsChampion -v   # (bot deleted; git history)
```
