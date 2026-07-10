# Chapter 2 — The discard value network

*2026-07-08. Code: `ml/cribml/data.py`, `ml/cribml/model.py`,
`ml/scripts/train_discard.py`. Result: 96.1%/94.1% agreement with the exact
evaluator, 0.003/0.004 pts/hand regret, from a 30k-parameter net trained in
40 seconds.*

## Lesson 1: encoding — where domain knowledge sneaks in

The net can't eat "KS, 6S, 9H, 8C". Turning cards into numbers is the first
real design decision, and the naive answer — six slots of (rank, suit)
numbers — fails twice:

- **A hand is a set, not a sequence.** With slots, `(KS, 6S, …)` and
  `(6S, KS, …)` are different inputs that must produce the same output; the
  net wastes capacity learning permutation invariance we can grant for free.
- **Rank is not a magnitude.** A King is not "13× an Ace": for pips K equals
  T, for runs K neighbors Q. Feeding rank as a number imposes a fake linear
  structure the net must first unlearn.

The canonical encoding (`data.py`, and the Go bot must mirror it exactly):
a **105-dim vector** — 52-dim multi-hot for the kept four, 52-dim multi-hot
for the thrown two, 1 dealer flag. Card index = `4*(rank−1) + suit`, suits
CDHS = 0..3, matching `internal/cribbage`. Multi-hot makes set-invariance
structural, and one-hot ranks let the net learn what each rank *means*.
No engineered features (pip values, flush flags…) in v1 — deliberately,
to see how far the raw representation gets. (Answer: far.)

## Lesson 2: leakage — split by hand, never by row

The 15 splits of one hand share six cards; they are heavily correlated
examples. Randomly assigning rows to train/val would put siblings of every
val row in training, and every metric would flatter. We split **by hand**
(last 8,000 of 120,000 held out). Residual overlap from independent deals
colliding on the same 6 cards: expected ≈44 of 8,000 val hands (0.6%) also
appear somewhere in train — negligible, noted for honesty.

## Lesson 3: the loss is not the metric

Training minimizes **MSE** on predicted split value (a smooth, optimizable
surrogate). But the bot doesn't need accurate values — it needs the argmax
of 15 predictions to be right, and when wrong, cheaply wrong. So we report
decision metrics: **agreement** (net's argmax = exact argmax) and **regret**
(exact value of best split minus exact value of the chosen one; the champion
scores 0 by definition). A net can have mediocre MAE and perfect regret —
value offsets that are constant within a hand cancel in the argmax.

## The run

30,209 parameters (105→128→128→1), Adam 1e-3, batch 4096, 20 epochs over
224k hand-seat examples ×15 splits; every split seen from both seats so the
net must read the dealer flag. ~2s/epoch on Apple-silicon MPS.

| | untrained (smoke) | epoch 5 | epoch 20 |
|---|---|---|---|
| val MAE (pts) | 5.5 | 0.22 | 0.075 |
| agreement dealer/pone | 7%/7% | 86%/82% | **96.1%/94.1%** |
| regret dealer/pone (pts/hand) | 3.6/3.8 | 0.068/0.069 | **0.0028/0.0037** |

Reading it: by point-EV lights the net is behaviorally indistinguishable
from the exact evaluator — it gives up ~1 point per ~300 deals. Note
agreement is consistently worse for the pone while regret stays comparable:
pone disagreements cluster where `ehand − crib_ev` nearly ties, so the
mistakes that remain are the cheap ones. Also note val MAE bounces around
(0.061→0.122 across late epochs) while regret improves monotonically —
the decision metric is what matters and it's more stable than the surrogate.

## Why this worked so easily — and why that was the point

This is, by ML standards, an *easy* problem: exact noise-free labels, a
small discrete input space (~20M hands), millions of cheap examples, and a
smooth target (expected values, mostly additive in hand structure with
combinatorial spikes the net has capacity to memorize). Phase 1 was chosen
because success or failure would be unambiguous. The pipeline — generator,
encoding, trainer, exporter, Go parity — is now debugged on a problem where
nothing could hide. Phase 2 (self-play pegging RL) has none of these
luxuries; that's where the machinery gets stress-tested.

## Reproduce

```
go run ./cmd/mldata -n 120000 -seed 1 -out ml/data/discard-120k.jsonl
cd ml && uv run scripts/train_discard.py --data data/discard-120k.jsonl --out runs/discard-v1
```

Weights land in `ml/runs/discard-v1/weights.json` (git-ignored; regenerate
as above). Exact byte-reproducibility is not guaranteed across torch
versions/devices — the metrics are what should reproduce.

## Next (Chapter 3)

Wire the net into a lab bot: Go-side encoder mirroring `data.py` (with a
cross-language encoding parity test), `nn.MLP` inference for the 15 splits,
champion pegging. Then the real exam: full-game `bot.Compare` against the
champion — where the 0.003-point discard gap should be invisible and any
loss would indicate a wiring bug, not a learning failure.
