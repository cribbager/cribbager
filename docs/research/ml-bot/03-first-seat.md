# Chapter 3 — The net takes a seat

*2026-07-08. Code: `internal/bot/lab/mlbot.go` (+ tests), `ml/scripts/make_bot_parity.py`.*

The chapter 2 network only ever answered quiz questions. This chapter puts it
in a chair: `ml-discard`, a lab challenger that discards by running fifteen
forward passes (one per split) and throwing the argmax, and pegs with the
champion's play. Champion pegging isn't a cop-out — isolating one learned
decision per experiment is the design; a compare that changed both discard
and pegging at once wouldn't tell us *which* was responsible for the result.

## Cross-language paranoia, part two

Chapter 1's parity test proved Go's *forward pass* matches PyTorch. But the
bot adds a second translation: turning cards into the 105-dim input. If the
Go encoder disagreed with `data.py` — suits ordered differently, keep/discard
blocks swapped — the net would receive gibberish *from a correct forward
pass*, and nothing would crash. Silent-wrongness again, so the same medicine
again: `make_bot_parity.py` emits 360 fixture cases (split → exact hot
indices + predicted value, the value computed by a third, independent numpy
forward over the exported weights file), and `TestMLDiscardParity` holds the
Go chain to them within 1e-6. Green on first run.

The encoding now exists in two places by design (`data.py` documents it, Go
mirrors it); the fixture is what keeps "by design" true after any retrain or
refactor — regenerating it is part of `make_bot_parity.py`, which also
installs the weights into `lab/testdata/`.

## Predictions (written before the gate ran)

By construction this bot cannot beat its teacher on points; the question is
how little it loses, and where. On the 10,000-pair duplicate gate we predict:

1. **Margin ≈ −0.06 pts/pair** — the chapter 2 regret (~0.003 pts per
   decision) times ~18 discard decisions per pair. Well inside even the
   10k-pair CI (~±0.13): statistically invisible, as near-perfect imitation
   should be.
2. **WinDiff ≈ −0.011 wins/pair** — the net is score-blind, so it forfeits
   precisely the endgame edge that promoted champion v3 over v2
   (+0.011 wins/pair). The 10k-pair CI is ~±0.009, so this may just barely
   resolve as significant — a measurable price for ignoring the score.

## Results — one prediction wrong, and wrong in the best way

10,000 deal-pairs (20,000 games):

| instrument | predicted | observed |
|---|---|---|
| paired margin | ≈ −0.06 pts/pair, invisible | **+0.42 pts/pair, CI [+0.26, +0.58] — significantly POSITIVE** |
| paired windiff | ≈ −0.011 wins/pair | **−0.005 wins/pair, CI [−0.011, +0.000] — borderline negative** |

The win prediction landed (score-blindness costs about half a point of win
rate; the CI's upper edge sits exactly at zero). The points prediction was
not just off — it had the wrong *sign*, significantly. The student
out-scored the teacher.

Did the net learn to discard better than the exact evaluator it imitated?
No — and seeing why is the chapter's real lesson. The −0.06 prediction
modeled the champion as a point-EV discarder. It isn't. Near the target,
champion v3 deliberately *spends* points to buy win probability (that trade
is precisely what promoted v3 over v2). `ml-discard`, score-blind, never
pays that premium — so across thousands of games it harvests the points the
champion intentionally sacrifices, +0.42/pair of them, and forfeits what
those points were buying: −0.005 wins/pair. Both numbers are the same
phenomenon measured by instruments pointed at different objectives.

So the gate's automated verdict — "BETTER on points, wins not worse —
promote" — is a trap here, and the two-instrument rule's own fine print
says so: for score-aware comparisons the points instrument is structurally
blind (worse: anti-correlated). Champion v3 would lose the points
instrument to champion v2 for the same reason. **Call: do not promote.**
`ml-discard` stays in the lab as the Phase 1 deliverable — a proven
pipeline and a baseline for later chapters. (Observation for the lab: the
verdict heuristic could check whether the challenger is score-aware before
recommending promotion on points; the borderline windiff CI [−0.011,
+0.000] technically passes "not significantly worse" while being exactly
consistent with a real −0.005 cost.)

What the gate did certify: over 20,000 full games the whole learned chain —
encoding, weights, forward pass, argmax — played flawlessly at production
speed, and its point-EV discarding is statistically indistinguishable from
exact away from the endgame. Phase 1 is complete.

## Reproduce

```
cd ml && uv run scripts/make_bot_parity.py       # install weights + fixture
go test ./internal/bot/lab -run MLDiscard -v      # parity + behavior
CHALLENGE=ml-discard PAIRS=10000 go test ./internal/bot/lab -run ChallengerVsChampion -v
```
