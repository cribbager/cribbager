# The ML bot program

An experiment: can a learned (neural-network) bot beat champion v3 — specifically
at pegging? This directory is both the program log and a tutorial narrative:
each chapter explains the ML concepts as they're used, records the decisions
made, and shows the experimental results, so it doubles as "learn ML via
cribbage".

**The bar.** Champion v3's discard play is near-exact expected value, so we do
not expect to beat it there — the discard phase is our supervised-learning
training ground precisely *because* we can measure a net against known-optimal
answers. Pegging is different: the champion plays one-ply search against a
calibrated opponent belief, which is strong but not provably optimal. That is
the target. Success = a learned pegging policy that beats champion v3 in a
pegging-isolated duplicate-deal comparison; stretch = a hybrid challenger
(champion discards + ML pegging) passing the standard two-instrument promotion
gate (see the bot improvement program's log — a local research note, not
published alongside these chapters).

## Decisions (2026-07-08)

- **Training stack: standard tooling.** Python + PyTorch, in an `ml/` directory
  in this repo. Training runs locally.
- **Inference: pure Go, hand-written forward pass.** The trained network's
  weights are exported to a flat file and the lab bot runs a small hand-written
  matrix-multiply forward pass (~50 lines, zero new Go dependencies). Writing
  it is itself a chapter: nothing demystifies a neural net like implementing
  inference by hand. (Alternative considered: ONNX runtime — rejected as a
  heavy dependency for what is a tiny MLP.)
- **The bot develops in `internal/bot/lab`** like any challenger, and is only
  promoted to a named production bot when it earns it. Server code never
  imports lab.
- **Champion stays the default opponent** unless/until the ML bot unseats it.
- Infrastructure prerequisite (in flight, separate PR): multiple named
  production bots, server-selectable per game, bot name+version persisted with
  finished games.

## The plan

### Phase 0 — Scaffolding
`ml/` directory with a uv-managed Python environment (PyTorch, numpy). A Go
data-generation tool (`cmd/` or a lab helper) that deals hands and emits
training examples using the engine and the champion's exact evaluators. A
weights file format + Go forward-pass loader in lab, tested against reference
outputs from PyTorch so we know the two implementations agree bit-for-bit
(well, float-for-float).

*Concepts: the train-in-Python / infer-in-Go split; reproducibility; why data
pipelines are most of real ML work.*

### Phase 1 — Supervised discard value network (the training ground)
Learn a function: (4 kept cards, 2 discarded cards, am-I-dealer) → expected
value. At decision time, score all 15 splits of the 6-card hand and take the
argmax. The champion's exact EV tables are a perfect, infinite label source —
a luxury real ML problems never have, and exactly why we start here: every
question ("did the net learn?", "how wrong is it?") has a ground-truth answer.

Deliberately **score-blind**: inputs are cards + dealer flag only. The point-EV
target does not depend on the score, so score inputs here would only teach the
net to ignore them; game-situation awareness enters when the label becomes win
probability (Phase 4). This does NOT mean the program settles for score-blind
discarding — see "The discard question" below.

- Card/feature encoding (one-hot ranks & suits; what richer features buy).
- MLP architecture, MSE regression loss, Adam, train/val/test discipline.
- Metrics: mean EV gap vs exact; argmax agreement %; EV lost per deal when
  the net disagrees (disagreement is fine if the EV cost is tiny).
- Deliverable: a lab bot playing full games with net discards + champion
  pegging, measured with `bot.Compare` against the champion.

*Concepts: supervised learning end-to-end — features, capacity, overfitting,
learning curves, generalization.*

### Phase 2 — Pegging by self-play reinforcement learning (the experiment)
No teacher this time: pegging has hidden information (opponent's cards) and
sequential consequences (the card you lead shapes the whole series), and the
champion's one-ply search is beatable in principle. The net learns from
outcomes of playing against itself.

- State encoding: count, series cards, all cards seen, own remaining hand,
  scores; action = which legal card to play (or go).
- Reward design: start with pegging-point differential; graduate toward the
  win-probability objective.
- Algorithm: start simple (policy gradient / DQN), escalate to PPO only if
  needed. Self-play loop with periodic frozen-opponent evaluation.
- Evaluation harness: pegging-isolated duplicate-deal compare — both sides
  discard with champion policy, only the Play policy differs, so the
  measurement isolates pegging skill.

*Concepts: MDPs and partial observability, value vs policy methods,
exploration, self-play dynamics, reward shaping, why RL is famously finicky.*

### Phase 3 — Assembly and the gate
Compose the results into a single challenger. If Phase 2 clears its bar, run
the hybrid (champion discards + ML pegging) through the standard promotion
gates. Either outcome — promotion or a documented negative result — completes
the core experiment.

### Phase 4 (contingent) — outcome-learned discard
Point the Phase 2 self-play machinery at the discard decision, trained on
full-game outcomes. This is the only route to discarding *better* than the
champion (distilling `RankDiscardsWin` could at best tie its teacher), and it
targets a specific, verified blind spot — see "The discard question". Hardest
credit assignment in the program (a discard's effect on the win is buried
under ~20 later decisions), hence last.

## The discard question: is imitating point EV the wrong goal?

Examined 2026-07-08 (owner's challenge: shouldn't the bot learn risk-taking
when behind, defensive discards when ahead, pegging-aware keeps?). Findings:

- **Score-aware risk appetite exists in champion v3 — where the walk engages,
  and with caveats.** Once either player could mathematically cross the target
  this deal, `RankDiscardsWin` walks each hold's full score *distribution*
  through the deal in counting order and ranks by P(win) — bold-when-behind
  and safe-when-ahead fall out of the math, and v3's promotion (+0.011
  wins/pair full-game, +0.011 pooled positional fixtures) is aggregate
  evidence it helps. Three qualifications (examined 2026-07-08):
  (1) *early game is pure point-EV* — the fast path rests on P(win) being
  "effectively affine" in points across a deal (the code's own hedge); par-hole
  theory (Colvert's Theory of 26, the repo's own par benchmark) says the true
  surface has curvature experts play from deal one; (2) *the walk's inputs are
  modeled marginals* (fixed pegging joint, opponent hand without card-removal
  conditioning, component independence), so its risk appetite is directionally
  sound but uncalibrated at the single-decision level; (3) *validation is
  aggregate*, not decision-level. Point-EV imitation (Phase 1) remains a
  training ground, not the final discard policy.
- **Pegging-aware discarding is a real, structural champion blind spot.** In
  `holdWinProb` (windiscard.go), the pegging distribution (`pegJointDist`) is
  a fixed self-play marginal applied identically to all 15 holds — the
  evaluator cannot see that keeping A-2-3-4 pegs differently from K-K-Q-5
  ("component independence is an accepted v1 approximation"). An
  outcome-trained discard (Phase 4) is the credible way to exploit this.
- **Sequencing stands**: exact-label supervision (Phase 1) debugs the tooling
  where failures are unambiguous; short-horizon RL (Phase 2) builds the
  self-play machinery; only then is full-game credit assignment (Phase 4)
  tractable to debug.

Running list of hypotheses where learning could beat the champion:
1. Series-level pegging tactics (Phase 2) — one-ply search can't see whole-series
   traps. **CONFIRMED 2026-07-08**: ml-peg +0.70 pts/pair [+0.46, +0.95] on the
   pegging-isolated gate, virgin deals, win-diff non-negative (chapter 5).
2. Pegging-aware keeps (Phase 4) — `pegJointDist` is hold-independent.
   **CONFIRMED AND SHIPPED 2026-07-10** (ml v2): residual pegging net worth
   ≈ +0.40 pts/pair over the exact evaluator on the discard-isolated gate,
   +0.008 wins/pair pooled fixtures (chapter 7).
3. Early-game positional discarding (Phase 4) — the point-EV fast path assumes
   an affine win surface; par-hole curvature, if real and learnable, lives here.
   **REJECTED 2026-07-10** at this method and scale (chapter 8): win-delta
   learning measured significantly worse on both instruments (−0.58 pts,
   −0.011 wins vs ml); the affine assumption survives its first learned
   challenge.

## Chapters

- [01 — Scaffolding: the pipeline and the hand-written forward pass](01-scaffolding.md)
- [02 — The discard value network: encoding, leakage, decision metrics](02-discard-net.md)
- [03 — The net takes a seat: lab bot, cross-language parity, the gate](03-first-seat.md)
- [04 — Pegging: no more teacher (MDP framing, MC Q-learning, round 0)](04-pegging-rl.md)
- [05 — The iteration loop, a stall, a leak, and the bar](05-iteration.md)
- [06 — Phase 3: assembly, a dead safety net, and promotion](06-promotion.md)
- [07 — Phase 4: the discard learns from outcomes (a failure, a fix, ml v2)](07-outcome-discard.md)
- [08 — Hypothesis 3: positional discarding — a clean negative closes the ledger](08-positional-discard.md)

## Log

- **2026-07-08** — Program started. Decisions above; multi-bot infrastructure
  landed as PR #47 (bot registry, server-selectable opponent, `GET /bots`,
  bot name+version persisted and restored with sessions).
- **2026-07-08** — Chapter 1 complete: `internal/nn` forward pass with
  PyTorch parity test green, `ml/` uv+PyTorch environment, `cmd/mldata`
  exact-labeled discard data generator.
- **2026-07-08** — Examined the point-EV-imitation assumption (see "The
  discard question"). Outcome: sequencing stands; Phase 1 stays score-blind
  by design; added contingent Phase 4 (outcome-learned discard) targeting the
  champion's hold-independent-pegging blind spot.
- **2026-07-08** — Chapter 2 complete: discard net trained. 96.1%/94.1%
  argmax agreement with the exact evaluator, 0.0028/0.0037 pts/hand regret
  (dealer/pone) on 8k held-out hands; 30k params, 40s on MPS. Code merged
  via the ml-bot branch PR; docs remain local (docs/research/ is git-ignored
  by design — open question whether to publish the ml-bot chapters).
- **2026-07-08** — Chapter 3 complete, **Phase 1 done**: `ml-discard` lab bot
  (net discards + champion pegging), 360-case cross-language parity green.
  10k-pair gate: +0.42 pts/pair (significant) but −0.005 wins/pair
  (borderline) — the net harvests the points the champion deliberately spends
  on win probability near the target, and forfeits the wins those points buy.
  Not promoted; kept as lab baseline. Lesson: the points instrument is
  anti-correlated with score-aware play; the gate's auto-verdict recommended
  promotion and would have been wrong.
- **2026-07-08** — Chapter 4 complete, **Phase 2 round 0**: pegging RL
  environment (`peg` package: 128-dim encoder, rank actions, MC returns from
  the event log), `peg-random` floor measured at −21.71 pts/pair vs champion,
  Q₁ trained on 973k random self-play decisions → `ml-peg` at −2.89 pts/pair
  (87% of the gap closed in one round). Next: the iteration loop.
- **2026-07-08** — Chapter 5: **THE BAR IS CLEARED.** Iteration stalled at
  round 2 (fresh-net drift), pooling rounds 1+2 flipped the sign, a
  train-on-test seed overlap was caught and eliminated (gate `SEED` override),
  and the clean 10k-pair gate reads +0.70 pts/pair [+0.46, +0.95] with
  win-diff +0.007 [−0.000, +0.015]. Hypothesis 1 confirmed. Not promoted:
  Phase 3 (assembly + positional fixtures) is the promotion path.
- **2026-07-09** — Chapter 6, **Phase 3 complete: production bot `ml`
  shipped** (champion discards + learned pegging, weights embedded).
  Fixtures cleared the score-blind fear (pooled +0.002 [−0.004, +0.008]);
  the InReach handoff hybrid measured strictly worse than pure net pegging
  (+0.28 vs +0.70 pts/pair) and was deleted. peg package moved to
  internal/bot/peg; fixtures gate gained SEED. Champion remains the default
  opponent — unseating it is a product decision, still open.
- **2026-07-10** — Chapter 7, **Phase 4: hypothesis 2 confirmed, ml v2
  shipped.** Whole-deal outcome learning failed honestly (−11.83 pts/pair,
  diagnosed as label-noise drowning); residual learning (net predicts only
  the keep's pegging differential, added to exact Score) flipped it to
  +0.78 pts/pair at win-parity. Control (ev-discard) decomposed the margin:
  ≈ +0.40 of genuine pegging-awareness above the +0.38 harvest. Fixtures
  INVERTED the score-blind fear: +0.008 wins/pair pooled, +0.043 at 118-118.
  InReach handoff destroyed value for discards (+0.13, n.s.) — opposite of
  its pegging verdict. Whole package vs champion: +0.75 pts/pair AND +0.011
  wins/pair — first win on both instruments. Gates gained BASELINE. H3 still
  open.
- **2026-07-10** — Chapter 8, **hypothesis 3 rejected; the ledger closes.**
  Win-delta target (one-deal bootstrap through the WinProb table, uncensored,
  ml-v2 behavior policy, 1.53M decisions): the position-aware discard
  measured WORSE on both instruments (−0.58 pts, −0.011 wins vs ml at 20k
  pairs) — noise-hallucinated deviations from the exact+peg ordering. The
  negative is method-and-scale-bounded; equipment (win mode, DiscardInputWin)
  kept. Program status: H1 confirmed+shipped, H2 confirmed+shipped, H3
  falsified. The commissioned experiment is complete.
- **2026-07-10** — Epilogue: **ml is now the default opponent**
  (`bot.DefaultName`; the champion remains available by name and stays the
  lab's reference baseline — legacy games still restore against it), and
  these chapters are **published**: docs/research/ml-bot is carved out of the
  docs/research/ ignore rule. If you're reading this on GitHub, that's why.
